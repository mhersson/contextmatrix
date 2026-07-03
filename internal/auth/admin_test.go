package auth

import (
	"context"
	"testing"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUserWithInvite_RoundTrip(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	user, invite, err := svc.CreateUserWithInvite(ctx, "Carol", "Carol C", false)
	require.NoError(t, err)
	assert.Equal(t, "carol", user.Username)
	assert.False(t, user.IsAdmin)
	assert.Nil(t, user.PasswordHash)

	// The returned invite token redeems to a working login.
	redeemed, session, err := svc.RedeemInviteOrReset(ctx, invite, "carols password1")
	require.NoError(t, err)
	assert.Equal(t, "carol", redeemed.Username)

	res, err := svc.ValidateSession(ctx, session)
	require.NoError(t, err)
	assert.Equal(t, "carol", res.User.Username)
}

func TestLastAdminGuard(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "root", "root password1", true)
	seedUser(t, svc, store, "user", "user password1", false)

	require.ErrorIs(t, svc.SetUserAdmin(ctx, "root", false), ErrLastAdmin)
	require.ErrorIs(t, svc.SetUserDisabled(ctx, "root", true), ErrLastAdmin)

	// With a second active admin both operations succeed.
	require.NoError(t, svc.SetUserAdmin(ctx, "user", true))
	require.NoError(t, svc.SetUserAdmin(ctx, "root", false))

	root, err := store.UserByUsername(ctx, "root")
	require.NoError(t, err)
	assert.False(t, root.IsAdmin)
}

func TestSetUserDisabled_KillsSessions(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "root", "root password1", true)
	seedUser(t, svc, store, "alice", "alice password1", false)

	_, session, err := svc.Login(ctx, "alice", "alice password1", "1.2.3.4")
	require.NoError(t, err)

	require.NoError(t, svc.SetUserDisabled(ctx, "alice", true))

	_, err = svc.ValidateSession(ctx, session)
	require.ErrorIs(t, err, ErrSessionInvalid)

	// Re-enabling does not resurrect old sessions.
	require.NoError(t, svc.SetUserDisabled(ctx, "alice", false))

	_, err = svc.ValidateSession(ctx, session)
	require.ErrorIs(t, err, ErrSessionInvalid)
}

func TestRegenerateLink_PurposeByPasswordState(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	invited, _, err := svc.CreateUserWithInvite(ctx, "fresh", "", false)
	require.NoError(t, err)

	raw, purpose, err := svc.RegenerateLink(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeInvite, purpose, "no password yet → invite")
	assert.NotEmpty(t, raw)

	seedUser(t, svc, store, "hazpw", "haz password12", false)

	_, purpose, err = svc.RegenerateLink(ctx, "hazpw")
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeReset, purpose, "password set → reset")

	_ = invited
}

func TestPatchGuard_MultiFieldNoPartialApply(t *testing.T) {
	// Demote+rename in one conceptual patch: when the demote is refused
	// (last admin), the display name must not have been applied either.
	// The handler evaluates guards before applying — this test pins the
	// service-level building block it uses.
	svc, store, _ := newTestService(t)
	ctx := context.Background()

	seedUser(t, svc, store, "root", "root password1", true)

	err := svc.SetUserAdmin(ctx, "root", false)
	require.ErrorIs(t, err, ErrLastAdmin)

	got, err := store.UserByUsername(ctx, "root")
	require.NoError(t, err)
	assert.True(t, got.IsAdmin, "refused demote leaves the flag untouched")
}

func TestAdminOps_UnknownUser(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	require.ErrorIs(t, svc.SetUserAdmin(ctx, "ghost", true), authstore.ErrNotFound)

	_, _, err := svc.RegenerateLink(ctx, "ghost")
	assert.ErrorIs(t, err, authstore.ErrNotFound)
}
