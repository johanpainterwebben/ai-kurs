//go:build !mock

package main

import (
	"errors"
	"log/slog"

	"temperatur/internal/obs"
)

// newMockSource finns inte i produktionsbygget: de sparade svaren ingår inte
// i binären, så TNU_MOCK=1 kan inte slås på av misstag på servern.
func newMockSource(*slog.Logger) (obs.Source, error) {
	return nil, errors.New("TNU_MOCK=1 kräver ett bygge med -tags mock")
}
