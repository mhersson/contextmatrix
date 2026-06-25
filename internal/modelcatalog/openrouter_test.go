package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchORCatalogParsesPriceWindowTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"z-ai/glm-5.2","context_length":1048576,
			 "pricing":{"prompt":"0.0000012","completion":"0.0000041"},
			 "supported_parameters":["tools","temperature"]},
			{"id":"some/no-tools","context_length":8192,
			 "pricing":{"prompt":"0.000001","completion":"0.000002"},
			 "supported_parameters":["temperature"]}
		]}`))
	}))
	defer srv.Close()

	cat, err := fetchORCatalog(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	e, ok := cat["z-ai/glm-5.2"]
	if !ok {
		t.Fatal("glm-5.2 missing")
	}

	if !e.Tools || e.ContextWindow != 1048576 || e.PromptPrice != 0.0000012 || e.CompletionPrice != 0.0000041 {
		t.Errorf("bad OR parse: %+v", e)
	}

	if cat["some/no-tools"].Tools {
		t.Error("no-tools model marked tools-capable")
	}
}
