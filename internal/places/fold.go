package places

import (
	"regexp"
	"strings"
)

var foldReplacer = strings.NewReplacer(
	"å", "a", "ä", "a", "ö", "o", "æ", "a", "ø", "o",
	"é", "e", "è", "e", "ü", "u",
	"-", " ", "/", " ", "_", " ", ",", " ", ".", " ", "(", " ", ")", " ",
)

// fold gör text jämförbar oavsett skiftläge, å/ä/ö och skiljetecken:
// "Göteborg/Tuve-Norum" blir "goteborg tuve norum".
func fold(s string) string {
	return strings.Join(strings.Fields(foldReplacer.Replace(strings.ToLower(s))), " ")
}

func withAliases(folded string) string {
	for _, w := range strings.Fields(folded) {
		if a, ok := aliases[w]; ok {
			folded += " " + a
		}
	}
	return folded
}

// kommunNames ger de namn en kommun kan sökas på exakt:
// "Lunds kommun" → "lunds kommun", "lunds", "lund".
func kommunNames(label string) []string {
	base := strings.TrimSuffix(fold(label), " kommun")
	names := []string{fold(label), base}
	if s, ok := strings.CutSuffix(base, "s"); ok {
		names = append(names, s)
	}
	if ort, ok := irregularKommuner[base]; ok {
		names = append(names, ort)
	}
	return names
}

// ortSuffix är tillägg som skiljer flera mätpunkter på samma ort åt:
// "Bollnäs 2", "Lysekil2", "Mölnbo (VV)", "Sigtuna vv".
var ortSuffix = regexp.MustCompile(`(?i)\s*(\d+|\(.*\)|\bvv)\s*$`)

// splitTitle delar en mätpunkts namn i ort och plats inom orten, utan
// tillägg: "Luleå/Hertsön 2" → "Luleå", "Hertsön". Plats är tom om namnet
// saknar snedstreck.
func splitTitle(title string) (ort, plats string) {
	ort, plats, _ = strings.Cut(title, "/")
	clean := func(s string) string { return strings.TrimSpace(ortSuffix.ReplaceAllString(s, "")) }
	return clean(ort), clean(plats)
}

func ortName(title string) string {
	ort, _ := splitTitle(title)
	return ort
}
