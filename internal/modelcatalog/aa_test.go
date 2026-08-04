package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFetchAAModelsFollowsPaginationAndMapsCreators(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("missing x-api-key, got %q", r.Header.Get("x-api-key"))
		}

		if r.URL.Query().Get("tier") != "free" {
			t.Errorf("endpoint query params must survive page construction, got %q", r.URL.RawQuery)
		}

		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = w.Write([]byte(`{"data":[
				{"slug":"glm-5-2","model_creator":{"name":"Z AI"},
				 "evaluations":{"artificial_analysis_coding_index":68.8,"artificial_analysis_intelligence_index":50.9}},
				{"slug":"img-only","model_creator":{"name":"Alibaba"},
				 "evaluations":{"artificial_analysis_coding_index":null,"artificial_analysis_intelligence_index":null}}
			],"pagination":{"page":1,"page_size":200,"total_pages":2,"has_more":true},"tier":"free"}`))
		case "2":
			_, _ = w.Write([]byte(`{"data":[
				{"slug":"novel-1","model_creator":{"name":"Frontier Labs Ltd."},
				 "evaluations":{"artificial_analysis_coding_index":40,"artificial_analysis_intelligence_index":30}}
			],"pagination":{"page":2,"page_size":200,"total_pages":2,"has_more":false},"tier":"free"}`))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	models, err := fetchAAModels(context.Background(), srv.URL+"?tier=free", "k")
	if err != nil {
		t.Fatal(err)
	}

	if len(models) != 3 {
		t.Fatalf("want 3 models merged across pages, got %d", len(models))
	}

	if models[0].Slug != "glm-5-2" || models[0].Creator != "z-ai" {
		t.Errorf("bad parse: %+v", models[0])
	}

	if models[0].CodingIndex == nil || *models[0].CodingIndex != 68.8 {
		t.Errorf("coding index not parsed: %+v", models[0].CodingIndex)
	}

	if models[1].CodingIndex != nil {
		t.Errorf("null coding index must stay nil, got %+v", models[1].CodingIndex)
	}

	if models[1].Creator != "qwen" {
		t.Errorf("Alibaba must resolve to qwen, got %q", models[1].Creator)
	}

	if models[2].Creator != "frontier-labs-ltd" {
		t.Errorf("unknown creator must slugify, got %q", models[2].Creator)
	}
}

func TestFetchAAModelsEmptyCatalogErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"pagination":{"page":1,"page_size":200,"total_pages":1,"has_more":false}}`))
	}))
	defer srv.Close()

	if _, err := fetchAAModels(context.Background(), srv.URL, "k"); err == nil {
		t.Fatal("empty catalog must error, got nil")
	}
}

func TestFetchAAModelsPageCapStopsRunaway(t *testing.T) {
	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = w.Write([]byte(`{"data":[{"slug":"m-1","model_creator":{"name":"OpenAI"},
			"evaluations":{"artificial_analysis_coding_index":50,"artificial_analysis_intelligence_index":50}}],
			"pagination":{"has_more":true}}`))
	}))
	defer srv.Close()

	models, err := fetchAAModels(context.Background(), srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}

	if got := requests.Load(); got != aaMaxPages {
		t.Errorf("want exactly %d page requests, got %d", aaMaxPages, got)
	}

	if len(models) != aaMaxPages {
		t.Errorf("want %d models (one per page), got %d", aaMaxPages, len(models))
	}
}

func TestFetchAAModelsPageFailureFailsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(`{"data":[{"slug":"m-1","model_creator":{"name":"OpenAI"},
			"evaluations":{"artificial_analysis_coding_index":50,"artificial_analysis_intelligence_index":50}}],
			"pagination":{"has_more":true}}`))
	}))
	defer srv.Close()

	if _, err := fetchAAModels(context.Background(), srv.URL, "k"); err == nil {
		t.Fatal("mid-pagination failure must fail the whole fetch, got nil")
	}
}
