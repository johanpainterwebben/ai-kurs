# Åtgärdsplan för kodanalysen: filer, ändringar och ordning

Datum: 2026-10-28. **Inget av detta är genomfört.** Planen bygger på fynden i
[kodanalys-teknisk-skuld-2026-10-28.md](kodanalys-teknisk-skuld-2026-10-28.md); siffrorna i
parentes (F1–F20) pekar på numren där.

Varje steg är tänkt som en egen PR som kan granskas och rullas tillbaka för sig. Efter varje steg
gäller kontrollerna sist i dokumentet.

## Översikt

| Steg | Mål | Fynd | Storlek |
|---|---|---|---|
| 1 | Persistera femminutersregeln och timbudgeten | F1 | liten |
| 2 | Källneutral modell, mättid som `time.Time` | F2, F4 | medel |
| 3 | Per-källa-schemaläggare | F3 | medel |
| 4 | Dela upp `match.go`, typad poäng, `Kind`-konstant | F5–F8 | medel |
| 5 | Frontend: ren logik, temperaturtabell, delade konstanter | F9–F11, F15 | medel |
| 6 | Server och `main` | F12, F13 | liten |
| 7 | Hygien, Docker och CI | F14, F16–F20 | liten–medel |

Ordningen är vald så att steg 1 minskar en verklig risk mot API-villkoren, steg 2–3 görs innan en
andra källa skrivs, steg 4 är rent internt och täcks av befintliga tester, och steg 5 görs
tillsammans med eller före Svelte-flytten.

---

## Steg 1 – Persistera femminutersregeln och timbudgeten (F1)

**Varför först:** Kravet "även efter omstarter" i CLAUDE.md uppfylls inte i dag.

**Filer som påverkas**

| Fil | Ändring |
|---|---|
| `internal/places/index.go` | Läs `lastAttempt` från tillståndsfilen vid start och skriv den när en hämtning påbörjas (före anropet, så att en krasch under anropet också räknas). |
| `internal/tnu/client.go` | Läs och skriv `calls` (osignerade anrop senaste timmen) till samma tillstånd. |
| nytt: `internal/state/state.go` | Litet paket: `Load(path)`, `Save(path, v)` med atomisk skrivning (tempfil + rename) och tolerans för saknad/trasig fil (börja då från noll, men logga). |
| `main.go` | Ny env `STATE_DIR` (standard `/data`); skicka in sökvägen. |
| `docker-compose.yml` | Montera en namngiven volym på `/data` (filsystemet förblir `read_only` i övrigt). |
| `Dockerfile` | Skapa `/data` med rätt ägare för `nonroot`. |
| `internal/places/index_test.go`, `internal/tnu/client_test.go` | Nya tester: ny `Index`/`Client` mot samma tillståndsfil gör ingen hämtning inom fem minuter / inom budgeten. |
| `README.md`, `DEPLOY.md` | Dokumentera volymen och `STATE_DIR`. |

**Beslut att ta:** vid trasig eller saknad tillståndsfil, ska servern vänta fem minuter innan första
hämtningen (säkrast) eller hämta direkt (snabbast start)? Rekommendation: hämta direkt bara om filen
helt saknas (förstagångsstart), annars vänta.

## Steg 2 – Källneutral modell och riktig mättid (F2, F4)

**Filer som påverkas**

| Fil | Ändring |
|---|---|
| nytt: `internal/obs/obs.go` (eller liknande) | `Station`/`Observation` med `Source`, `ID`, `Title`, `Temp *float64`, `Lat`, `Lon`, `Kommun`, `Lan`, `MeasuredAt time.Time`, `URL`. Gemensamt fel `ErrBudgetExhausted`. Gränssnitt `Source` flyttas hit. |
| `internal/tnu/client.go` | Mappa `apiResponse` → `obs.Station`; parsa `lastUpdate` med `Europe/Stockholm`; ta bort `Station` och `Source` härifrån; returnera `obs.ErrBudgetExhausted` (behåll `tnu.ErrBlocked` internt). |
| `internal/tnu/mock.go` | Följer med mappningen. |
| `internal/places/index.go`, `match.go` | Byt `tnu.Station` mot `obs.Station`; `places` slutar importera `tnu`. |
| `internal/server/server.go` | Importera `obs` i stället för `tnu` för felet. JSON-fältet `lastUpdate` blir ISO-UTC (`measuredAt`). |
| `web/app.js` | `formatUpdated` tar ISO-tid i stället för strängjämförelse; byt fältnamn. |
| Tester i `internal/tnu`, `internal/places`, `internal/server` | Anpassas till de nya typerna; nytt test för sommar-/vintertid i tidsparsningen. |

**Notera:** detta är en ändring av API-svaret (`lastUpdate` → `measuredAt`). Frontend och backend
måste ändras i samma PR.

## Steg 3 – Per-källa-schemaläggare (F3)

**Filer som påverkas**

| Fil | Ändring |
|---|---|
| nytt: `internal/sched/` (eller `internal/source/`) | Generisk schemaläggare per källa: intervall, minsta mellanrum, timbudget, felräknare/stale, persistens (från steg 1), panikskydd, egen cache med senast kända data och ålder. |
| `internal/places/index.go` | Tappar ansvaret för tid, fel och räknare; lyssnar på schemaläggarens resultat och bygger snapshot. Tar emot en eller flera källor. |
| `internal/tnu/client.go` | Behåller bara anrop, signering och tolkning; budgeten flyttas till schemaläggaren. |
| `internal/places/index_test.go` | De befintliga testerna (falsk klocka m.m.) flyttas till schemaläggarens tester; `Index` får färre. |
| `CLAUDE.md` | Uppdatera avsnittet "Flera källor": gränssnitt och var reglerna bor. |

**Risk:** `index_test.go` innehåller i dag de viktigaste garantierna (inga omförsök inom intervallet,
panik blir fel, delad första hämtning). De ska flyttas, inte skrivas om, så att inga garantier
tappas.

## Steg 4 – Dela upp `match.go` (F5–F8)

Rent internt; publika API:t för `places` ändras inte, utom `Kind`.

| Fil | Ändring |
|---|---|
| `internal/places/match.go` | Delas i `snapshot.go` (`newSnapshot` uppdelad i gruppera / slå ihop / bestäm detalj / sortera), `fold.go` (`fold`, alias, `splitTitle`, `kommunNames`), `rank.go` (`Search`, `resolve`, `Suggest`, `rank`) och `matcher.go` (`matcher`, `score`, `prefixDistance`). |
| samma | Poängen blir en struct (`kind`, `typos`, `viaOrt`) med `less`; `fuzzy` härleds på ett ställe. |
| samma | `type Kind string` med konstanterna `KindKommun`, `KindOrt`, `KindPlats`; `kindOrder` blir en metod. |
| `internal/places/data.go` (ny, eller en JSON-fil via `go:embed`) | `aliases` och `irregularKommuner`. |
| `internal/places/match_test.go` | Delas på samma sätt (`snapshot_test.go`, `matcher_test.go`). Ett test per alias/oregelbunden kommun. |
| `internal/places/bench_test.go` | Oförändrad; körs före och efter. |

**Krav:** `go test -run xxx -bench Suggest ./internal/places/` ska ligga kvar kring 0,2–0,4 ms och
samma antal allokeringar.

## Steg 5 – Frontend (F9–F11, F15)

Görs före eller tillsammans med Svelte-flytten.

| Fil | Ändring |
|---|---|
| nytt: `web/src/lib/temp.js` | Temperaturskalan som en tabell: nivå, gräns, klass, kläder och text, så att `tempLevel`, `OUTFITS` och texterna härleds ur samma data. |
| nytt: `web/src/lib/format.js` | `formatTemp`, `formatFetched`, `formatUpdated`, `joinWith`. |
| nytt: `web/src/lib/url.js` | `isTnuURL` och reservlänken (en konstant). |
| nytt: `web/src/lib/limits.js` | `MIN_QUERY`, `MAX_QUERY`; Go-sidan ger samma värden via `/api/config` eller en genererad fil, så att de inte skrivs två gånger. |
| `web/app.js` → Svelte-komponenter | `SearchBox` (combobox och förslag), `Summary`, `StationList`, `Figure` (gubben), `StaleNotice`. State i en liten store i stället för modulvariabler. |
| `web/style.css` | De 29 reglerna för temperaturnivåer genereras eller delas upp per komponent; färgerna ligger i en tokens-fil. |
| `contrast_test.go` | Läser tokens-filen i stället för att tolka CSS med regex; flyttas till egen testkatalog. |
| nytt: JS-tester (Vitest) | Täcker `temp.js`, `format.js`, `url.js`. |
| `internal/server/server.go` (`staticHandler`) | Anpassas till hashade filnamn: långt `Cache-Control: immutable` för hashade filer, `no-cache` bara för `index.html`. |
| `Dockerfile` | Frontend-byggsteg (Node) före Go-bygget; ingen Node i slutimagen. |

**CSP:** kontrollera `transition:`/`animate:` och alla Svelte-funktioner som lägger in stilmallar
vid körning mot `style-src 'self'`.

## Steg 6 – Server och `main` (F12, F13)

| Fil | Ändring |
|---|---|
| `internal/server/server.go` | Delas i `handlers.go`, `static.go`, `headers.go`. En hjälpfunktion för mönstret query → snapshot → cache-rubrik → JSON. Namngivna konstanter för `Cache-Control`. Felmeddelandet byggs av `minQueryLen`/`maxQueryLen`. |
| samma | `/healthz` rapporterar om listan någonsin hämtats och om den är inaktuell (utan att läcka detaljer). `writeJSON` loggar kodningsfel. |
| nytt: `internal/config/config.go` | `Load()` läser och validerar env (`REFRESH_INTERVAL`, `TNU_*`, `ADDR`, `STATE_DIR`); returnerar en struct. |
| `main.go` | `run` blir kort: `config.Load`, `buildSources(cfg)`, uppstart, nedstängning. |
| `internal/server/server_test.go`, nytt `internal/config/config_test.go` | Tester för `/healthz`-tillstånden och för env-tolkningen (för kort intervall, ogiltigt värde, saknad `TNU_CLI`). |

## Steg 7 – Hygien, Docker och CI (F14, F16–F20)

| Fil | Ändring |
|---|---|
| `internal/tnu/mock.go` | Bakom byggtagg (`//go:build mock`) eller flyttat till ett testpaket; `TNU_MOCK` ska inte fungera i produktionsbygget. |
| `internal/places/match_test.go` (efter steg 4) | Små handgjorda fixturer för reglerna; behåll några tester mot verklig mock-data. |
| `CLAUDE.md` | Konvention för språk i identifierare. |
| `internal/tnu/client.go` | Kommentar vid `sign` om att MD5 föreskrivs av API:et. |
| `Dockerfile` | Kopiera `go.mod`/`go.sum` först och kör `go mod download` (cache), `HEALTHCHECK`, pinna basimagerna med digest. |
| `docker-compose.yml` | Ta bort `ports: 127.0.0.1:8080` (eller dokumentera undantaget i CLAUDE.md). |
| nytt: `.golangci.yml` | `staticcheck`, `errcheck`, `gosec`, `govet`. |
| nytt: `.github/workflows/ci.yml` | Enligt CLAUDE.md: `go vet`, `go test -race`, benchmarken, `govulncheck`, frontendbygge, `svelte-check`, lint, `npm audit`, kontrasttestet, Docker-bygge med Trivy. `permissions` minimala, actions låsta till commit-SHA. |
| nytt: `.github/workflows/deploy.yml` | Vid merge till `main`: bygg, tagga med SHA, publicera på GHCR, driftsätt, kontrollera `/healthz`, rulla tillbaka vid fel. |

---

## Kontroller efter varje steg

Enligt CLAUDE.md ska varje större ändring granskas och rapporteras:

- `go vet ./...` och `go test -race ./...`
- `go test -run xxx -bench Suggest ./internal/places/` (0,2–0,4 ms, några KB per anrop)
- `contrast_test.go` vid alla färgändringar
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (och `npm audit` när frontend byggs)
- `git ls-files` visar ingen `.env` eller `__debug_bin*`
- Gränssnittsändringar: skärmbilder i ljust och mörkt läge och i mobilbredd, inga CSP-fel i konsolen
- API-villkoren: samma fråga högst var femte minut, även efter omstart (testat från steg 1)

## Öppna frågor

1. Steg 1: ska servern vänta fem minuter efter omstart om tillståndsfilen är trasig?
2. Steg 2: ska `lastUpdate` byta namn i API:et direkt, eller ska båda fälten finnas en tid?
3. Steg 5: ska Svelte-flytten göras före steg 4, eller ska frontendlogiken lyftas ut i vanlig JS först?
4. Steg 7: ska `docker-compose.yml` sluta publicera `127.0.0.1:8080`, eller ska CLAUDE.md ändras?
