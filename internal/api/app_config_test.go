package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAppConfig(t *testing.T) {
	tests := []struct {
		name          string
		theme         string
		wantTheme     string
		wantStatus    int
		wantCTHeader  string
	}{
		{
			name:         "everforest theme",
			theme:        "everforest",
			wantTheme:    "everforest",
			wantStatus:   http.StatusOK,
			wantCTHeader: "application/json",
		},
		{
			name:         "radix theme",
			theme:        "radix",
			wantTheme:    "radix",
			wantStatus:   http.StatusOK,
			wantCTHeader: "application/json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &appConfigHandlers{theme: tc.theme}

			req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
			w := httptest.NewRecorder()

			h.getAppConfig(w, req)

			res := w.Result()
			defer closeBody(t, res.Body)

			assert.Equal(t, tc.wantStatus, res.StatusCode)
			assert.Contains(t, res.Header.Get("Content-Type"), tc.wantCTHeader)

			var got appConfigResponse
			require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
			assert.Equal(t, tc.wantTheme, got.Theme)
		})
	}
}
