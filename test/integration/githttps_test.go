//go:build integration

package integration_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const gitHTTPBackendPath = "/usr/lib/git-core/git-http-backend"

// startGitHTTPS spins up an HTTPS server in front of git-http-backend.
//
// All bare git repos under parentDir become reachable from the worker
// container as `https://host.docker.internal:<port>/<repo>.git`. The
// server uses an ephemeral self-signed cert; callers must set
// GIT_SSL_NO_VERIFY=1 in the worker env so the agent's git client
// accepts the unverified TLS handshake.
//
// Listens on 0.0.0.0 (not 127.0.0.1) so the docker bridge gateway can
// reach it — the worker's loopback isn't the host's loopback. The
// validator's hostRE regex accepts "host.docker.internal" and the
// orchestrated dispatcher already wires
// `host.docker.internal:host-gateway` into ExtraHosts.
//
// Returns the base URL (e.g. "https://host.docker.internal:43137") and
// registers t.Cleanup to shut down the server.
func startGitHTTPS(t *testing.T, parentDir string) string {
	t.Helper()

	cert := selfSignedCert(t)

	handler := &cgi.Handler{
		Path: gitHTTPBackendPath,
		Env: []string{
			"GIT_PROJECT_ROOT=" + parentDir,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=harness",
		},
	}

	listener, err := tls.Listen("tcp", "0.0.0.0:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("git-https listen: %v", err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = srv.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = srv.Close()
	})

	port := listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("https://host.docker.internal:%d", port)
}

// selfSignedCert produces an ephemeral cert valid for the duration of
// the test. The harness never validates it; the worker container
// short-circuits via GIT_SSL_NO_VERIFY=1.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "cm-integration-harness"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.ParseIP("::1")},
		DNSNames:     []string{"host.docker.internal", "localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
}

// repoNameFromURL extracts the basename of the git repo from a URL.
// Used for harness-side post-test git inspections (e.g. counting commits
// in the bare). Strips the trailing ".git" if present.
func repoNameFromURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	return strings.TrimSuffix(name, ".git")
}

// enableHTTPReceivePack flips the bare repo's `http.receivepack` flag
// so git-http-backend serves smart-HTTP push requests. Without this
// the worker's `git push` returns "service not enabled".
func enableHTTPReceivePack(t *testing.T, bareDir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", bareDir, "config", "http.receivepack", "true")
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("enable http.receivepack: %v", err)
	}
}
