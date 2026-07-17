//go:build integration

// Package integration_test is a thin smoke harness for ContextMatrix and its
// two backends. It boots the real CM binary in auth.mode: multi and drives a
// small set of scenarios end to end:
//
//   - TestMultiUserAdminSurface / TestChatREST - backend-free; need only CM.
//   - TestAgentScenario - the contextmatrix-agent backend runs a card to done
//     against a scripted LLM and a seeded git server.
//   - TestChatScenario - the contextmatrix-chat backend answers one chat
//     message from the scripted LLM.
//
// TestMain builds only CM; the sibling agent/chat repos and their minimal
// worker images are built lazily (ensureAgentAssets / ensureChatAssets) and
// skip with a clear message when a sibling checkout is absent. Gated behind the
// `integration` build tag so `make test` ignores it. Requires Docker.
package integration_test
