//go:build integration

// Package integration_test runs end-to-end harness tests of CM + runner
// + a stub or real worker image. Gated behind the `integration` build
// tag so `make test` ignores it. See
// docs/superpowers/specs/2026-05-03-integration-harness-long-lived-session-design.md.
package integration_test
