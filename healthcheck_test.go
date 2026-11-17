package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	if err := healthcheck(addr); err != nil {
		t.Fatalf("friska servern: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := healthcheck(addr); err == nil {
		t.Fatal("503 ska ge fel")
	}
	if err := healthcheck("ogiltig"); err == nil {
		t.Fatal("ogiltig adress ska ge fel")
	}
	ts.Close()
	if err := healthcheck(addr); err == nil {
		t.Fatal("stängd server ska ge fel")
	}
}
