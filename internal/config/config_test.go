package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TNU_CLI": "min@epost.se"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mock || cfg.CLI != "min@epost.se" || cfg.CLIKey != "" ||
		cfg.RefreshInterval != DefaultRefreshInterval || cfg.Addr != DefaultAddr {
		t.Errorf("fick %+v", cfg)
	}
}

func TestLoadMockNeedsNoCLI(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TNU_MOCK": "1"}))
	if err != nil || !cfg.Mock {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadRequiresCLIWithoutMock(t *testing.T) {
	for _, mock := range []string{"", "0", "true"} { // bara "1" slår på mock
		_, err := Load(env(map[string]string{"TNU_MOCK": mock}))
		if err == nil || !strings.Contains(err.Error(), "TNU_CLI") {
			t.Errorf("TNU_MOCK=%q: err = %v, vill ha fel om TNU_CLI", mock, err)
		}
	}
}

func TestLoadRefreshInterval(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", DefaultRefreshInterval},
		{"15m", 15 * time.Minute},
		{"5m", MinRefreshInterval},
		{"1m", MinRefreshInterval}, // under femminutersgränsen höjs till gränsen
		{"0s", MinRefreshInterval},
		{"-1h", MinRefreshInterval},
	}
	for _, tt := range tests {
		cfg, err := Load(env(map[string]string{"TNU_MOCK": "1", "REFRESH_INTERVAL": tt.in}))
		if err != nil || cfg.RefreshInterval != tt.want {
			t.Errorf("REFRESH_INTERVAL=%q: %v, %v; vill ha %v", tt.in, cfg.RefreshInterval, err, tt.want)
		}
	}
}

func TestLoadRejectsInvalidInterval(t *testing.T) {
	_, err := Load(env(map[string]string{"TNU_MOCK": "1", "REFRESH_INTERVAL": "snart"}))
	if err == nil || !strings.Contains(err.Error(), "REFRESH_INTERVAL") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadAddrAndKey(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TNU_CLI": "x", "TNU_CLI_KEY": "hemlig", "ADDR": "127.0.0.1:9000"}))
	if err != nil || cfg.Addr != "127.0.0.1:9000" || cfg.CLIKey != "hemlig" {
		t.Errorf("cfg=%+v err=%v", cfg, err)
	}
}
