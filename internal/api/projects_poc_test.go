package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateProject_POC_SetPreserveClear(t *testing.T) {
	server := noneModeServer(t)

	on := validUpdateProjectBody(nil)
	on.POC = new(true)
	resp := putProject(t, server.URL, nil, on)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, decodeProject(t, resp).POC)
	closeBody(t, resp.Body)

	resp = putProject(t, server.URL, nil, validUpdateProjectBody(nil))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, decodeProject(t, resp).POC, "omitting poc must preserve it")
	closeBody(t, resp.Body)

	off := validUpdateProjectBody(nil)
	off.POC = new(false)
	resp = putProject(t, server.URL, nil, off)

	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.False(t, decodeProject(t, resp).POC, "poc false must switch it off")
}
