// Package llmchanneltest builds LLM channel holders for tests.
package llmchanneltest

import (
	"context"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
)

// Store is an llmchannel.Store with no stored mode that accepts every write.
type Store struct{}

func (Store) LLMChannel(context.Context) (string, time.Time, string, error) {
	return "", time.Time{}, "", nil
}

func (Store) PutLLMChannel(context.Context, string, string) error { return nil }

func (Store) RecordSubscriptionUsage(context.Context, []protocol.SubscriptionUsage, time.Time) error {
	return nil
}

// EndpointOnly returns the holder production builds when claude_code is
// unset: every start runs in endpoint mode on ep, and a nil ep sends none.
func EndpointOnly(ep *protocol.LLMEndpoint) *llmchannel.Holder {
	h, err := llmchannel.New(context.Background(), llmchannel.Config{Endpoint: ep}, Store{}, events.NewBus())
	if err != nil {
		// Store never fails a read, so New cannot fail.
		panic(err)
	}

	return h
}
