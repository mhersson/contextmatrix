package chat

import "context"

// BuildResumeForTest is a test-only export of buildResume, allowing
// package chat_test to exercise the tail-loading behaviour without
// accessing unexported methods directly.
func (m *Manager) BuildResumeForTest(ctx context.Context, sessionID string) *ResumeContext {
	return m.buildResume(ctx, sessionID)
}

// SetRehydrationActiveForTest is a test-only export of setRehydrationActive.
func (m *Manager) SetRehydrationActiveForTest(ctx context.Context, sessionID string, active bool) error {
	return m.setRehydrationActive(ctx, sessionID, active)
}

// RehydrationActiveCacheForTest reads only the in-memory cache value under
// m.mu, returning (value, present). Unlike isRehydrationActive, it does not
// fall back to the store on miss — tests use it to assert that the cache
// reflects exactly what setRehydrationActive committed, without masking a
// store/cache divergence by silently re-populating from disk.
func (m *Manager) RehydrationActiveCacheForTest(sessionID string) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.rehydrationActive[sessionID]

	return v, ok
}

// ConsumePendingToolUseIDForTest is a test-only export of
// consumePendingToolUseID, allowing package chat_test to assert pending-ID
// state without going through SendUserMessage side-effects.
func (m *Manager) ConsumePendingToolUseIDForTest(sessionID string) string {
	return m.consumePendingToolUseID(sessionID)
}
