package chat_test

import (
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/service"
)

// Compile-time assertion: *service.CardService satisfies chat.Pricer.
var _ chat.Pricer = (*service.CardService)(nil)
