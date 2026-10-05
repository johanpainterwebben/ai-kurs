// Kommandot temperatur startar webbservern för temperatursöket.
//
// Inställningar via miljövariabler:
//
//	TNU_CLI      klient-id mot temperatur.nu (obligatoriskt utan TNU_MOCK;
//	             osignerat ska det vara din e-postadress enligt API-villkoren)
//	TNU_CLI_KEY  hemlig klientnyckel; om den finns signeras alla anrop
//	TNU_MOCK=1   använd sparade svar i stället för det riktiga API:et
//	REFRESH_INTERVAL  hur ofta listan med mätpunkter förnyas i bakgrunden, minst 5m (standard 10m)
//	ADDR         adress att lyssna på (standard :8080)
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"temperatur/internal/places"
	"temperatur/internal/server"
	"temperatur/internal/tnu"
)

// minInterval är API-villkorets gräns: samma fråga högst var femte minut.
const minInterval = 5 * time.Minute

//go:embed web
var webFiles embed.FS

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("avslutar", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	interval := 10 * time.Minute
	if v := os.Getenv("REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		interval = max(d, minInterval)
	}

	var src tnu.Source
	if os.Getenv("TNU_MOCK") == "1" {
		m, err := tnu.NewMock()
		if err != nil {
			return err
		}
		src = m
		log.Info("använder sparade svar (TNU_MOCK=1)")
	} else {
		cli := os.Getenv("TNU_CLI")
		if cli == "" {
			return errors.New("TNU_CLI saknas (sätt TNU_MOCK=1 för att köra utan API:et)")
		}
		c := tnu.NewClient(cli, os.Getenv("TNU_CLI_KEY"), nil)
		if c.Signed() {
			log.Info("signerar anrop mot temperatur.nu")
		} else {
			log.Warn("osignerade anrop – begränsat antal anrop per timme", "max_per_timme", tnu.UnsignedHourlyLimit)
		}
		src = c
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Hämta mätpunkterna direkt och förnya dem sedan i bakgrunden, så att
	// besökare alltid läser från cachen och aldrig väntar på temperatur.nu.
	index := places.NewIndex(src, interval, log)
	go index.Run(ctx)

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	s, err := server.New(index, static, log)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		MaxHeaderBytes:    16 << 10, // räcker gott för GET-anrop; standard är 1 MB
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("lyssnar", "addr", addr, "refresh_interval", interval)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
