// Kommandot temperatur startar webbservern för temperatursöket.
//
// Inställningarna läses från miljövariabler (se package config).
//
//	temperatur              startar servern
//	temperatur healthcheck  anropar /healthz på den egna servern och avslutar
//	                        med 0 om den svarar; används av Docker HEALTHCHECK
package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"temperatur/internal/config"
	"temperatur/internal/feed"
	"temperatur/internal/obs"
	"temperatur/internal/places"
	"temperatur/internal/server"
	"temperatur/internal/smhi"
	"temperatur/internal/tnu"
	"temperatur/internal/wind"
)

//go:embed web
var webFiles embed.FS

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := os.Getenv("ADDR")
		if addr == "" {
			addr = config.DefaultAddr
		}
		if err := healthcheck(addr); err != nil {
			log.Error("healthcheck misslyckades", "err", err)
			os.Exit(1)
		}
		return
	}
	if err := run(log); err != nil {
		log.Error("avslutar", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	src, err := newSource(cfg, log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Hämta mätpunkterna direkt och förnya dem sedan i bakgrunden, så att
	// besökare alltid läser från cachen och aldrig väntar på temperatur.nu.
	index := places.NewIndex(src, cfg.RefreshInterval, log)
	go index.Run(ctx)

	// Vinden från SMHI hämtas separat. Om SMHI är nere ligger den senaste
	// vinden kvar, och utan vind är "Känns som" samma som temperaturen.
	feelsLike, err := newFeelsLike(ctx, log)
	if err != nil {
		return err
	}

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	s, err := server.New(index, feelsLike, static, log)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.Handler(),
		MaxHeaderBytes:    16 << 10, // räcker gott för GET-anrop; standard är 1 MB
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("lyssnar", "addr", cfg.Addr, "refresh_interval", cfg.RefreshInterval)
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

func newFeelsLike(ctx context.Context, log *slog.Logger) (places.FeelsLikeFunc, error) {
	mapping, err := wind.LoadMapping()
	if err != nil {
		return nil, err
	}
	client := smhi.NewClient(nil)
	fetch := func(ctx context.Context) (map[string]smhi.Wind, error) {
		winds, err := client.LatestWind(ctx)
		if err == nil {
			log.Info("hämtade vind från SMHI", "stationer", len(winds))
		}
		return winds, err
	}
	winds := feed.New("vind", smhi.RefreshInterval, fetch, log)
	go winds.Run(ctx)
	return wind.NewEstimator(mapping, winds.Latest).FeelsLike, nil
}

func newSource(cfg config.Config, log *slog.Logger) (obs.Source, error) {
	if cfg.Mock {
		return newMockSource(log)
	}
	c := tnu.NewClient(cfg.CLI, cfg.CLIKey, nil)
	if c.Signed() {
		log.Info("signerar anrop mot temperatur.nu")
	} else {
		log.Warn("osignerade anrop – begränsat antal anrop per timme", "max_per_timme", tnu.UnsignedHourlyLimit)
	}
	return c, nil
}
