//go:build mock

package main

import (
	"log/slog"

	"temperatur/internal/obs"
	"temperatur/internal/tnumock"
)

// newMockSource ger sparade svar i stället för temperatur.nu (TNU_MOCK=1).
func newMockSource(log *slog.Logger) (obs.Source, error) {
	m, err := tnumock.New()
	if err != nil {
		return nil, err
	}
	log.Info("använder sparade svar (TNU_MOCK=1)")
	return m, nil
}
