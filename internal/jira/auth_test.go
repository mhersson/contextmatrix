package jira

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthorizer_Headers(t *testing.T) {
	tests := []struct {
		name, mode, email, token string
		wantHeader               string
	}{
		{
			name: "basic", mode: "basic", email: "me+cm@acme.com", token: "tok:with:colons-æøå",
			wantHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte("me+cm@acme.com:tok:with:colons-æøå")),
		},
		{name: "bearer", mode: "bearer", token: "svc-key", wantHeader: "Bearer svc-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAuthorizer(tt.mode, tt.email, tt.token)
			require.NoError(t, err)

			req, err := http.NewRequest(http.MethodGet, "https://acme.atlassian.net/rest/api/3/myself", nil)
			require.NoError(t, err)
			require.NoError(t, a.Apply(req))
			assert.Equal(t, tt.wantHeader, req.Header.Get("Authorization"))

			if tt.mode == "basic" {
				raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(req.Header.Get("Authorization"), "Basic "))
				require.NoError(t, err)
				assert.Equal(t, tt.email+":"+tt.token, string(raw))
			}
		})
	}
}

func TestNewAuthorizer_Rejects(t *testing.T) {
	const secret = "s3cr3t-token"

	tests := []struct{ name, mode, email, token string }{
		{name: "basic without email", mode: "basic", token: secret},
		{name: "basic without token", mode: "basic", email: "me@acme.com"},
		{name: "bearer without token", mode: "bearer"},
		{name: "unknown mode", mode: "cookie", token: secret},
		{name: "empty mode", mode: "", token: secret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAuthorizer(tt.mode, tt.email, tt.token)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), secret)
		})
	}
}

func TestAuthorizer_FormattingHidesSecret(t *testing.T) {
	const secret = "s3cr3t-token"

	for _, mode := range []string{"basic", "bearer"} {
		a, err := NewAuthorizer(mode, "me@acme.com", secret)
		require.NoError(t, err)

		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(verb, a)
			assert.NotContains(t, out, secret, "verb %s", verb)
			assert.NotContains(t, out, base64.StdEncoding.EncodeToString([]byte("me@acme.com:"+secret)), "verb %s", verb)
		}

		c := NewClient("https://acme.atlassian.net", a, nil)

		var buf bytes.Buffer

		slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "client", c)

		outs := []string{buf.String()}
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			outs = append(outs, fmt.Sprintf(verb, c))
		}

		for _, out := range outs {
			assert.NotContains(t, out, secret, "mode %s", mode)
			assert.NotContains(t, out, base64.StdEncoding.EncodeToString([]byte("me@acme.com:"+secret)), "mode %s", mode)
		}
	}
}
