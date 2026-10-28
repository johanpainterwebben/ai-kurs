// Package config läser inställningarna från miljövariabler.
//
//	TNU_CLI           klient-id mot temperatur.nu (obligatoriskt utan TNU_MOCK;
//	                  osignerat ska det vara din e-postadress enligt API-villkoren)
//	TNU_CLI_KEY       hemlig klientnyckel; om den finns signeras alla anrop
//	TNU_MOCK=1        använd sparade svar i stället för det riktiga API:et
//	                  (kräver bygge med -tags mock)
//	REFRESH_INTERVAL  hur ofta listan med mätpunkter förnyas i bakgrunden,
//	                  minst 5m (standard 10m)
//	ADDR              adress att lyssna på (standard :8080)
package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	// MinRefreshInterval är API-villkorets gräns: samma fråga högst var femte minut.
	MinRefreshInterval = 5 * time.Minute

	DefaultRefreshInterval = 10 * time.Minute
	DefaultAddr            = ":8080"
)

// Config är programmets inställningar.
type Config struct {
	Mock            bool
	CLI             string
	CLIKey          string // hemlig; får aldrig loggas
	RefreshInterval time.Duration
	Addr            string
}

// Load läser inställningarna med getenv (t.ex. os.Getenv) och kontrollerar dem.
// Ett REFRESH_INTERVAL under MinRefreshInterval höjs till gränsen.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Mock:            getenv("TNU_MOCK") == "1",
		CLI:             getenv("TNU_CLI"),
		CLIKey:          getenv("TNU_CLI_KEY"),
		RefreshInterval: DefaultRefreshInterval,
		Addr:            DefaultAddr,
	}
	if v := getenv("REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("REFRESH_INTERVAL: %w", err)
		}
		cfg.RefreshInterval = max(d, MinRefreshInterval)
	}
	if v := getenv("ADDR"); v != "" {
		cfg.Addr = v
	}
	if !cfg.Mock && cfg.CLI == "" {
		return Config{}, errors.New("TNU_CLI saknas (sätt TNU_MOCK=1 för att köra utan API:et)")
	}
	return cfg, nil
}
