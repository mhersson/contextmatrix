//go:build integration

// Package integration_test runs end-to-end harness tests of CM + runner
// against a synthetic stub worker. Gated behind the `integration`
// build tag so `make test` ignores it.
package integration_test
