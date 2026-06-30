package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchEndpointCatalog(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","context_length":200000,
			 "pricing":{"prompt":"0.000003","completion":"0.000015"},
			 "capabilities":{"features":["streaming","tools"]}},
			{"id":"model-b","context_length":128000,
			 "pricing":{"prompt":"0.0000007","completion":"0.000003"},
			 "capabilities":{"features":["streaming"]}}
		]}`))
	}))
	defer srv.Close()

	out, err := fetchEndpointCatalog(context.Background(), srv.URL, "secret")
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", gotAuth)
	require.Contains(t, out, "model-a")
	assert.True(t, out["model-a"].Tools)
	assert.Equal(t, 200000, out["model-a"].ContextWindow)
	assert.InDelta(t, 0.000003, out["model-a"].PromptPrice, 1e-12)
	assert.False(t, out["model-b"].Tools)
}
