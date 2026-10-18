package main

import (
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

func TestChatWorkerImageFor_CleanCut(t *testing.T) {
	tests := []struct {
		name string
		re   *board.RemoteExecutionConfig
		want string
	}{
		{name: "no remote execution config", re: nil, want: ""},
		{
			name: "chat image set",
			re:   &board.RemoteExecutionConfig{ChatWorkerImage: "contextmatrix-chat-worker:go-node"},
			want: "contextmatrix-chat-worker:go-node",
		},
		{
			// The clean-cut pin: worker_image (task image) must never leak
			// into chat sessions - the image families are not interchangeable.
			name: "task image set but chat image empty",
			re:   &board.RemoteExecutionConfig{WorkerImage: "contextmatrix-agent-worker:go-node"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &board.ProjectConfig{RemoteExecution: tt.re}
			assert.Equal(t, tt.want, chatWorkerImageFor(p))
		})
	}
}

type fakeRates map[string]service.ModelRate

func (f fakeRates) ResolvedRate(model string) (service.ModelRate, bool) {
	rate, ok := f[model]

	return rate, ok
}

func TestArchitectPriceFor(t *testing.T) {
	price := architectPriceFor(fakeRates{
		"priced":   service.ModelRate{Prompt: 3e-6, Completion: 15e-6}.Resolved(),
		"unpriced": {},
	})

	tests := []struct {
		name   string
		model  string
		wantOK bool
		want   protocol.ModelPrice
	}{
		{name: "unknown model", model: "missing"},
		{name: "all-zero rate is no price", model: "unpriced"},
		{
			name:   "priced model",
			model:  "priced",
			wantOK: true,
			want: protocol.ModelPrice{
				PromptUSDPerMTok:     3,
				CompletionUSDPerMTok: 15,
				CacheReadUSDPerMTok:  0.3,
				CacheWriteUSDPerMTok: 3.75,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := price(tt.model)

			assert.Equal(t, tt.wantOK, ok)
			assert.InDelta(t, tt.want.PromptUSDPerMTok, got.PromptUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.want.CompletionUSDPerMTok, got.CompletionUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.want.CacheReadUSDPerMTok, got.CacheReadUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.want.CacheWriteUSDPerMTok, got.CacheWriteUSDPerMTok, 1e-9)
		})
	}
}

func TestPriceable(t *testing.T) {
	tests := []struct {
		name string
		rate service.ModelRate
		want bool
	}{
		{name: "zero prompt and completion", rate: service.ModelRate{CacheRead: 1e-6, CacheWrite: 1e-6}},
		{name: "prompt only", rate: service.ModelRate{Prompt: 1e-6}, want: true},
		{name: "completion only", rate: service.ModelRate{Completion: 1e-6}, want: true},
		// OpenRouter reports -1 for dynamically priced routers such as openrouter/auto.
		{name: "negative sentinel", rate: service.ModelRate{Prompt: -1, Completion: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, priceable(tt.rate))
		})
	}
}

func TestModelPriceFromRate(t *testing.T) {
	tests := []struct {
		name                               string
		rate                               service.ModelRate
		prompt, completion, cacheR, cacheW float64
	}{
		{
			name:       "default cache multipliers",
			rate:       service.ModelRate{Prompt: 3e-6, Completion: 15e-6}.Resolved(),
			prompt:     3,
			completion: 15,
			cacheR:     0.3,
			cacheW:     3.75,
		},
		{
			name:       "explicit cache rates scale too",
			rate:       service.ModelRate{Prompt: 1e-6, Completion: 2e-6, CacheRead: 0.5e-6, CacheWrite: 1.5e-6},
			prompt:     1,
			completion: 2,
			cacheR:     0.5,
			cacheW:     1.5,
		},
		{name: "zero rate stays zero", rate: service.ModelRate{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelPriceFromRate(tt.rate)

			assert.InDelta(t, tt.prompt, got.PromptUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.completion, got.CompletionUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.cacheR, got.CacheReadUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.cacheW, got.CacheWriteUSDPerMTok, 1e-9)
		})
	}
}

func TestArchitectResearch(t *testing.T) {
	assert.Nil(t, architectResearch(&config.ArchitectBackendConfig{}))
	assert.Nil(t, architectResearch(&config.ArchitectBackendConfig{Research: config.ResearchOff, ResearchAPIKey: "k"}))
	assert.Equal(t, &protocol.ResearchStart{Provider: "brave", APIKey: "k"},
		architectResearch(&config.ArchitectBackendConfig{Research: config.ResearchSearch, ResearchAPIKey: "k"}))
}
