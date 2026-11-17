package places

import (
	"slices"
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	if got := fold("  Göteborg/Tuve-Norum (VV) "); got != "goteborg tuve norum vv" {
		t.Errorf("fold = %q", got)
	}
}

func TestOrtName(t *testing.T) {
	tests := map[string]string{
		"Lund/Värpinge":       "Lund",
		"Bollnäs 2":           "Bollnäs",
		"Lysekil2":            "Lysekil",
		"Mölnbo (VV)":         "Mölnbo",
		"Sigtuna vv":          "Sigtuna",
		"Luleå/Hertsön 2":     "Luleå",
		"Junosuando Västra 2": "Junosuando Västra",
		"Södra Sandby":        "Södra Sandby",
	}
	for title, want := range tests {
		if got := ortName(title); got != want {
			t.Errorf("ortName(%q) = %q, vill ha %q", title, got, want)
		}
	}
}

func TestSplitTitle(t *testing.T) {
	tests := []struct{ title, ort, plats string }{
		{"Luleå/Hertsön 2", "Luleå", "Hertsön"},
		{"Lund", "Lund", ""},
		{"Mölnbo (VV)", "Mölnbo", ""},
		{"Alfta/Centrum", "Alfta", "Centrum"},
	}
	for _, tt := range tests {
		if ort, plats := splitTitle(tt.title); ort != tt.ort || plats != tt.plats {
			t.Errorf("splitTitle(%q) = %q, %q; vill ha %q, %q", tt.title, ort, plats, tt.ort, tt.plats)
		}
	}
}

func TestKommunNames(t *testing.T) {
	tests := map[string][]string{
		"Lunds kommun":   {"lunds kommun", "lunds", "lund"},
		"Uppsala kommun": {"uppsala kommun", "uppsala"},
	}
	for label, want := range tests {
		if got := kommunNames(label); !slices.Equal(got, want) {
			t.Errorf("kommunNames(%q) = %v, vill ha %v", label, got, want)
		}
	}
}

// Varje alias i data.go ska läggas till bredvid förkortningen.
func TestAliases(t *testing.T) {
	for short, long := range aliases {
		want := short + " " + long
		if got := withAliases(short); got != want {
			t.Errorf("withAliases(%q) = %q, vill ha %q", short, got, want)
		}
		if !strings.Contains(newMatchKey("Xx/"+short).str, long) {
			t.Errorf("sökning på %q ska hitta namn med %q", long, short)
		}
	}
}

// Varje oregelbunden kommun i data.go ska kunna sökas på ortens namn.
func TestIrregularKommuner(t *testing.T) {
	for base, ort := range irregularKommuner {
		if got := kommunNames(base + " kommun"); !slices.Contains(got, ort) {
			t.Errorf("kommunNames(%q) = %v, saknar %q", base+" kommun", got, ort)
		}
	}
}
