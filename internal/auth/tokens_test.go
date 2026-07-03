package auth

import (
	"context"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapFlow(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	raw, err := svc.IssueBootstrapToken(ctx)
	require.NoError(t, err)

	info, err := svc.InspectToken(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeBootstrap, info.Purpose)
	assert.Empty(t, info.Username)

	user, sessionRaw, err := svc.RedeemBootstrap(ctx, raw, "Morten", "Morten H", "a strong password")
	require.NoError(t, err)
	assert.Equal(t, "morten", user.Username)
	assert.True(t, user.IsAdmin, "bootstrap account is admin")

	res, err := svc.ValidateSession(ctx, sessionRaw)
	require.NoError(t, err)
	assert.Equal(t, "morten", res.User.Username, "redemption auto-logs-in")

	// Token burned.
	_, _, err = svc.RedeemBootstrap(ctx, raw, "other", "", "another password1")
	require.ErrorIs(t, err, ErrTokenSpent)

	// A second bootstrap token cannot mint another admin once users exist.
	raw2, err := svc.IssueBootstrapToken(ctx)
	require.NoError(t, err)

	_, _, err = svc.RedeemBootstrap(ctx, raw2, "sneaky", "", "another password1")
	require.ErrorIs(t, err, ErrNotBootstrappable)

	_, err = store.UserByUsername(ctx, "sneaky")
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}

func TestBootstrap_Validation(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	raw, err := svc.IssueBootstrapToken(ctx)
	require.NoError(t, err)

	_, _, err = svc.RedeemBootstrap(ctx, raw, "morten", "", "short")
	require.ErrorIs(t, err, ErrPasswordTooShort)

	_, _, err = svc.RedeemBootstrap(ctx, raw, "bad name!", "", "a strong password")
	require.ErrorIs(t, err, authstore.ErrInvalidUsername)

	// Failed validation must NOT burn the token.
	info, err := svc.InspectToken(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeBootstrap, info.Purpose)
}

func TestInviteFlow(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	admin := seedUser(t, svc, store, "admin", "admin password1", true)
	_ = admin

	invited, err := store.CreateUser(ctx, "carol", "Carol", false, svcNow)
	require.NoError(t, err)

	raw, err := svc.IssueInviteToken(ctx, invited.ID)
	require.NoError(t, err)

	info, err := svc.InspectToken(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeInvite, info.Purpose)
	assert.Equal(t, "carol", info.Username)

	// Regenerating invalidates the previous link.
	raw2, err := svc.IssueInviteToken(ctx, invited.ID)
	require.NoError(t, err)

	_, err = svc.InspectToken(ctx, raw)
	require.ErrorIs(t, err, ErrTokenInvalid)

	user, sessionRaw, err := svc.RedeemInviteOrReset(ctx, raw2, "carols password1")
	require.NoError(t, err)
	assert.Equal(t, "carol", user.Username)
	assert.NotEmpty(t, sessionRaw)

	_, _, err = svc.Login(ctx, "carol", "carols password1", "1.2.3.4")
	assert.NoError(t, err)
}

func TestResetFlow_KillsOtherSessions(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "old password12", false)

	_, oldSession, err := svc.Login(ctx, "alice", "old password12", "1.2.3.4")
	require.NoError(t, err)

	alice, err := store.UserByUsername(ctx, "alice")
	require.NoError(t, err)

	raw, err := svc.IssueResetToken(ctx, alice.ID)
	require.NoError(t, err)

	_, newSession, err := svc.RedeemInviteOrReset(ctx, raw, "new password123")
	require.NoError(t, err)

	_, err = svc.ValidateSession(ctx, oldSession)
	require.ErrorIs(t, err, ErrSessionInvalid, "reset kills pre-existing sessions")

	_, err = svc.ValidateSession(ctx, newSession)
	require.NoError(t, err)

	_, _, err = svc.Login(ctx, "alice", "old password12", "1.2.3.4")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestRedeem_DisabledUser(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "gone", "", false, svcNow)
	require.NoError(t, err)

	raw, err := svc.IssueResetToken(ctx, u.ID)
	require.NoError(t, err)

	require.NoError(t, store.SetDisabled(ctx, u.ID, true, svcNow))

	_, _, err = svc.RedeemInviteOrReset(ctx, raw, "a strong password")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestInspectToken_ExpiredAndUnknown(t *testing.T) {
	svc, store, clock := newTestService(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "alice", "", false, svcNow)
	require.NoError(t, err)

	raw, err := svc.IssueInviteToken(ctx, u.ID)
	require.NoError(t, err)

	*clock = clock.Add(OneTimeTokenTTL + time.Minute)

	_, err = svc.InspectToken(ctx, raw)
	require.ErrorIs(t, err, ErrTokenExpired)

	_, err = svc.InspectToken(ctx, "garbage")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestChangePassword(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "alice", "old password12", false)

	_, keep, err := svc.Login(ctx, "alice", "old password12", "1.2.3.4")
	require.NoError(t, err)

	_, other, err := svc.Login(ctx, "alice", "old password12", "5.6.7.8")
	require.NoError(t, err)

	alice, err := store.UserByUsername(ctx, "alice")
	require.NoError(t, err)

	require.ErrorIs(t, svc.ChangePassword(ctx, alice.ID, "wrong", "new password123", keep), ErrInvalidCredentials)
	require.ErrorIs(t, svc.ChangePassword(ctx, alice.ID, "old password12", "short", keep), ErrPasswordTooShort)

	require.NoError(t, svc.ChangePassword(ctx, alice.ID, "old password12", "new password123", keep))

	_, err = svc.ValidateSession(ctx, keep)
	require.NoError(t, err, "acting session survives")

	_, err = svc.ValidateSession(ctx, other)
	require.ErrorIs(t, err, ErrSessionInvalid, "other sessions killed")

	_, _, err = svc.Login(ctx, "alice", "new password123", "1.2.3.4")
	assert.NoError(t, err)
}

func TestBootstrap_CrossTokenRace(t *testing.T) {
	// Two DIFFERENT outstanding bootstrap tokens (restart re-issues) must not
	// mint two admins: the store-level guard is atomic with the insert.
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	raw1, err := svc.IssueBootstrapToken(ctx)
	require.NoError(t, err)

	raw2, err := svc.IssueBootstrapToken(ctx)
	require.NoError(t, err)

	_, _, err = svc.RedeemBootstrap(ctx, raw1, "first", "", "a strong password")
	require.NoError(t, err)

	_, _, err = svc.RedeemBootstrap(ctx, raw2, "second", "", "a strong password")
	require.ErrorIs(t, err, ErrNotBootstrappable)

	users, err := store.ListUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 1)
}
