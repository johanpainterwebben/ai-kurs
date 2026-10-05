package main

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Kontrollerar att färgerna i web/style.css är läsbara i både ljust och mörkt
// läge: all text minst 7:1 (WCAG AAA), kanter och fokusmarkering minst 3:1.
func TestColorContrast(t *testing.T) {
	css, err := os.ReadFile("web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	light, dark := themes(t, string(css))

	type pair struct {
		fg, bg string
		min    float64
	}
	pairs := []pair{
		{"text", "bg", 7}, {"text", "surface", 7}, {"text", "select", 7},
		{"text-2", "bg", 7}, {"text-2", "surface", 7}, {"text-2", "select", 7},
		{"link", "bg", 7}, {"link", "surface", 7},
		{"on-accent", "accent", 7}, {"on-accent", "accent-hover", 7},
		{"error-text", "error-bg", 7},
		{"warning-text", "warning-bg", 7},
		{"warning-border", "bg", 3},
		{"border", "bg", 3}, {"border", "surface", 3},
		{"focus", "bg", 3}, {"focus", "surface", 3}, {"focus", "select", 3},
		{"accent-border", "bg", 3},
	}
	for _, level := range []string{"frost", "cold", "cool", "mild", "warm", "hot"} {
		pairs = append(pairs, pair{"t-" + level + "-ink", "t-" + level + "-bg", 7})
	}

	for name, theme := range map[string]map[string]string{"ljust": light, "mörkt": dark} {
		for _, p := range pairs {
			fg, okFg := theme[p.fg]
			bg, okBg := theme[p.bg]
			if !okFg || !okBg {
				t.Errorf("%s: --%s eller --%s saknas", name, p.fg, p.bg)
				continue
			}
			if r := contrast(fg, bg); r < p.min {
				t.Errorf("%s: --%s (%s) mot --%s (%s) har kontrast %.2f, minst %.1f krävs", name, p.fg, fg, p.bg, bg, r, p.min)
			}
		}
	}
}

var (
	colorVar  = regexp.MustCompile(`--([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\b`)
	darkBlock = regexp.MustCompile(`(?s)@media \(prefers-color-scheme: dark\)\s*\{\s*:root\s*\{(.*?)\}`)
)

// themes läser färgvariablerna i första :root-blocket (ljust) och i blocket
// för mörkt läge, som skriver över de ljusa.
func themes(t *testing.T, css string) (light, dark map[string]string) {
	t.Helper()
	start := strings.Index(css, ":root {")
	if start < 0 {
		t.Fatal(":root saknas i style.css")
	}
	end := strings.Index(css[start:], "\n}")
	light = vars(css[start : start+end])

	m := darkBlock.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("blocket för mörkt läge saknas i style.css")
	}
	dark = map[string]string{}
	for k, v := range light {
		dark[k] = v
	}
	for k, v := range vars(m[1]) {
		dark[k] = v
	}
	return light, dark
}

func vars(block string) map[string]string {
	out := map[string]string{}
	for _, m := range colorVar.FindAllStringSubmatch(block, -1) {
		out[m[1]] = strings.ToLower(m[2])
	}
	return out
}

// contrast är WCAG 2:s kontrastförhållande mellan två färger.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func luminance(hex string) float64 {
	channel := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(1) + 0.7152*channel(3) + 0.0722*channel(5)
}
