package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAAModelsParsesIndices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("missing x-api-key, got %q", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(`{"data":[
			{"slug":"glm-5-2","model_creator":{"slug":"zai"},
			 "evaluations":{"artificial_analysis_coding_index":68.8,"artificial_analysis_intelligence_index":50.9}},
			{"slug":"img-only","model_creator":{"slug":"openai"},
			 "evaluations":{"artificial_analysis_coding_index":null,"artificial_analysis_intelligence_index":null}}
		]}`))
	}))
	defer srv.Close()

	models, err := fetchAAModels(context.Background(), srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].Slug != "glm-5-2" || models[0].Creator != "zai" {
		t.Errorf("bad parse: %+v", models[0])
	}
	if models[0].CodingIndex == nil || *models[0].CodingIndex != 68.8 {
		t.Errorf("coding index not parsed: %+v", models[0].CodingIndex)
	}
	if models[1].CodingIndex != nil {
		t.Errorf("null coding index must stay nil, got %+v", models[1].CodingIndex)
	}
}
