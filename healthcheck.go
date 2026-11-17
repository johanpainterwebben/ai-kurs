package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// healthcheck anropar /healthz på servern som lyssnar på addr. Imagen saknar
// skal och curl, så Docker HEALTHCHECK kör programmet självt.
func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get( //nolint:gosec // adressen byggs av vår egen konfiguration, aldrig av besökare
		"http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz svarade %s", res.Status)
	}
	return nil
}
