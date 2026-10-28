package places

// Uppslagsdata för namn som inte går att härleda ur mätpunkternas text. Varje
// post har ett test i fold_test.go eller snapshot_test.go.

// aliases lägger till sökord för vanliga förkortningar i mätpunkternas namn.
var aliases = map[string]string{
	"sthlm": "stockholm",
	"gbg":   "goteborg",
}

// irregularKommuner är kommuner vars ort inte heter som kommunen utan genitiv-s.
var irregularKommuner = map[string]string{
	"falu": "falun",
}
