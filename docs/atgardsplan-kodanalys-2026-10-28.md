# Åtgärdsplan för kodanalysen: filer, ändringar, ordning och status

Planerad 2026-10-28, status uppdaterad 2026-11-17. Planen bygger på fynden i
[kodanalys-teknisk-skuld-2026-10-28.md](kodanalys-teknisk-skuld-2026-10-28.md); siffrorna i
parentes (F1–F20) pekar på numren där. För genomförda steg beskrivs vad som faktiskt gjordes; för
steg som är kvar beskrivs planen.

Allt om CI/CD och driftsättning finns i [plan-cicd-2026-11-17.md](plan-cicd-2026-11-17.md).

Varje steg är tänkt som en egen PR som kan granskas och rullas tillbaka för sig. Efter varje steg
gäller kontrollerna sist i dokumentet.

## Översikt

| Steg | Mål | Fynd | Storlek | Status |
|---|---|---|---|---|
| 1 | Persistera femminutersregeln och timbudgeten | F1 | liten | **Kvar** |
| 2 | Källneutral modell, mättid som `time.Time` | F2, F4 | medel | Gjort |
| 3 | Schemaläggare per källa | F3 | medel | Gjort |
| 4 | Dela upp `match.go`, typad poäng, `Kind`-konstant | F5–F8 | medel | Gjort |
| 5 | Frontend: ren logik, temperaturtabell, delade konstanter | F9–F11, F15 | medel | **Kvar** |
| 6 | Server och `main` | F12, F13 | liten | Gjort |
| 7 | Hygien, Docker och CI | F14, F16–F20 | liten–medel | Gjort; resten av CI/CD i [plan-cicd](plan-cicd-2026-11-17.md) |

Ordningen är vald så att steg 1 minskar en verklig risk mot API-villkoren, steg 2–3 görs innan en
andra källa skrivs, steg 4 är rent internt och täcks av befintliga tester, och steg 5 görs
tillsammans med eller före Svelte-flytten.

---

## Steg 1 – Persistera femminutersregeln och timbudgeten (F1) – kvar

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

**Status:** Inte gjort. `Feed` och `Budget` (steg 3) är nu varsitt ställe att spara tillstånd från.
Samma brist finns som F11 i [sakerhetsgranskning-2026-10-28.md](sakerhetsgranskning-2026-10-28.md) och
punkt 1 i [granskning-mot-claude-md-2026-10-28.md](granskning-mot-claude-md-2026-10-28.md). Enligt
beslutet i [plan-cicd-2026-11-17.md](plan-cicd-2026-11-17.md) gäller kravet krascher, inte omstarter vid
driftsättning.

## Steg 2 – Källneutral modell och riktig mättid (F2, F4) – gjort

| Fil | Ändring |
|---|---|
| `internal/obs/obs.go` (ny) | `obs.Station` med `Source`, `MeasuredAt` (`time.Time`, UTC) och `URL`; gränssnittet `obs.Source`; felet `obs.ErrBudgetExhausted`. |
| `internal/tnu/client.go` | Översätter svaren till `obs.Station`. `lastUpdate` tolkas som svensk tid (`Europe/Stockholm`, `time/tzdata` inbäddad eftersom distroless saknar zoneinfo) och blir UTC. `tnu.Station`, `tnu.Source` och `tnu.ErrBudgetExhausted` är borttagna. Ny `DecodeStations`. Kommentar om att MD5 i `sign` är föreskrivet. |
| `internal/places/*`, `internal/server/*` | Använder `obs` i stället för `tnu`; `places` och `server` importerar inte längre `tnu`. |
| `web/app.js` | `lastUpdate` → `measuredAt`; `formatUpdated` visar ISO-tid i svensk tid. |
| `internal/tnu/client_test.go` | Nytt test för sommar-/vintertid och ogiltig tid. |

**API-ändring:** fältet `lastUpdate` heter nu `measuredAt` (ISO, UTC) och varje mätpunkt har `source`.

## Steg 3 – Schemaläggare per källa (F3) – gjort

| Fil | Ändring |
|---|---|
| `internal/feed/feed.go` (ny) | Generisk `Feed[T]`: intervall, aldrig tidigare omförsök än ett intervall (även efter fel), delad första hämtning, senast lyckade värdet kvar vid fel, inaktuell efter fler än 3 misslyckanden i rad, panikskydd, `Loaded()`. |
| `internal/feed/feed_test.go` (ny) | De tio garantitesterna från `index_test.go`, flyttade, plus ett test att två källor är oberoende. |
| `internal/budget/` (ny) | `Budget` räknar anrop per tidsfönster, med tester. Placerad separat, inte i feed, eftersom budgeten hör till en källas villkor. |
| `internal/tnu/client.go` | Använder `budget.Budget` (bara osignerat). |
| `internal/places/index.go` | `Index` är en tunn omslagsklass kring `Feed[*Snapshot]`; ny metod `Ready()`. |
| `internal/places/index_test.go` | Skrivet om till två små tester; schemaläggningen testas i `feed`. |

Sammanslagning av flera källor ingår inte; den byggs när en andra källa läggs till.

## Steg 4 – Uppdelning av `match.go` (F5–F8) – gjort

| Fil | Innehåll |
|---|---|
| `snapshot.go` | `Kind` (typad: `KindKommun/KindOrt/KindPlats`), `Snapshot`, `Place`; `newSnapshot` uppdelad i `groupStations`, `mergeSameNamedPlatser`, `kommunPlaces`, `ortPlaces`, `platsPlaces`, `finalize`. |
| `fold.go` | `fold`, `withAliases`, `kommunNames`, `splitTitle`, `ortName`. |
| `data.go` | `aliases` och `irregularKommuner`. |
| `rank.go` | `Search`, `resolve`, `Suggest`, `rank`. |
| `matcher.go` | `matchScore` (struct med klass, skrivfel och `viaOrt`, en `cmp`, `fuzzy()`), `matcher`. |
| `distance.go` | `prefixDistance` (oförändrad algoritm). |

Tester: `match_test.go` delad i `rank_test.go`, `fold_test.go`, `matcher_test.go` och nya handgjorda
fixturer i `snapshot_test.go`; ett datadrivet test per post i `data.go`.

**Prestanda (`go test -bench Suggest`)**

| | Före | Efter |
|---|---|---|
| Typiskt | 0,17 ms, 1 632 B | 0,17 ms, 1 384 B |
| Värsta fallet | 0,30–0,31 ms, 1 688 B | 0,30–0,38 ms, 1 688 B |

Spridningen i värsta fallet beror på kodplacering i minnet (samma algoritm; att flytta funktioner i den
gamla koden gav 0,30–0,35 ms). Inom målet 0,2–0,4 ms men nära taket.

## Steg 5 – Frontend (F9–F11, F15) – kvar

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

## Steg 6 – Server och `main` (F12, F13) – gjort

| Fil | Ändring |
|---|---|
| `internal/server/server.go` | Bara typer, konstanter, `New`, `Handler`. Namngivna `Cache-Control`-konstanter. |
| `internal/server/handlers.go` (ny) | Hanterare, hjälpfunktionen `respond` (delar mönstret query → lista → cache-rubrik → JSON), `writeJSON` som loggar fel, felmeddelandet byggs av `minQueryLen`/`maxQueryLen`. |
| `internal/server/static.go`, `headers.go` (nya) | Statiska filer respektive CSP och säkerhetsrubriker (oförändrat innehåll). |
| `/healthz` | Alltid 200, men texten är `ok`, `ok: väntar på första hämtningen` eller `ok: listan kan vara inaktuell`; `Cache-Control: no-store`. |
| `internal/config/` (ny) | `config.Load(getenv)` med validering och tester (`REFRESH_INTERVAL` höjs till minst 5 min, `TNU_CLI` krävs utan mock). |
| `main.go` | Kort `run`, `newSource(cfg, log)`, underkommandot `temperatur healthcheck`. |
| `healthcheck.go` | Anropar `/healthz` på den egna servern (för Docker `HEALTHCHECK`), med test. |
| `internal/server/server_test.go` | Nya tester: `/healthz` i tre tillstånd, `Cache-Control`, `source`/`measuredAt`, felmeddelandet. |

## Steg 7 – Hygien, Docker och CI (F14, F16–F20) – gjort

| Fil | Ändring |
|---|---|
| `internal/tnumock/` (ny; flyttad från `internal/tnu/mock.go` och `mockdata/`) | Sparade svar. Importeras bara av `mock_on.go` (`//go:build mock`); `mock_off.go` ger fel om `TNU_MOCK=1` utan taggen. Kontrollerat: produktionsbinären innehåller inget av mock-paketet. |
| `Dockerfile` | `go.mod` kopieras och moduler laddas ned före källkoden (cache); `HEALTHCHECK` via `/temperatur healthcheck`. Basimagerna är inte pinnade med digest (kunde inte hämtas utan Docker). |
| `.golangci.yml` (ny) | errcheck, govet, ineffassign, staticcheck, unused, gosec, bodyclose, gofmt. Två `//nolint:gosec` med motivering (MD5 i `sign`, lokal healthcheck-URL). |
| `.github/workflows/ci.yml` (ny) | Jobb: Go (gofmt, vet, `test -race`, benchmark, govulncheck), lint, Docker-bygge + Trivy. `permissions: {}` överst, `contents: read` per jobb, `pull_request` (inte `pull_request_target`), inga hemligheter. |
| `.github/workflows/publish.yml` (ny) | Efter merge till `main`: bygg, Trivy, publicera på GHCR taggad med commit-SHA. Bara `GITHUB_TOKEN` med `packages: write`. |
| `.github/dependabot.yml` (ny) | Veckovisa uppdateringar för Go-moduler, Actions och Docker. |
| Actions | Låsta till commit-SHA (version i kommentar); alla utgåvor äldre än två veckor. |
| `README.md`, `DEPLOY.md`, `CLAUDE.md`, `.vscode/launch.json` | `go run -tags mock .`, `buildFlags` för mock, nya `/healthz`-texter, avsnitt om CI/CD, uppdaterad arkitektur och namnkonvention (svenska domänbegrepp, engelska tekniska namn). |

Det som återstod av steget – låsa basimagerna med digest, ta bort den publicerade porten och
automatisk driftsättning (tidigare tänkt som `deploy.yml`) – finns nu i
[plan-cicd-2026-11-17.md](plan-cicd-2026-11-17.md).

## Verifiering av de genomförda stegen (2026-10-28)

- `go vet ./...` och `go vet -tags mock ./...`: rent.
- `go test ./...`: alla paket gröna.
- `staticcheck`: inga fynd. `govulncheck` (v1.9.0): inga sårbarheter.
- Rökprov av servern i mock-läge: `/healthz`, `/api/search`, `/api/suggest`, 400-svar, rubriker och
  `temperatur healthcheck` (exit 0 vid frisk server, 1 vid fel).
- `git ls-files`: ingen `.env` eller `__debug_bin*` incheckad. `cli`/`sign` loggas inte.

**Inte verifierat då:**

- `go test -race`: kräver C-kompilator och kunde inte köras lokalt; körs i CI.
- Docker-bygget, `golangci-lint` och workflow-filerna är inte körda. Första CI-körningen kan ge
  småanmärkningar.

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
2. Steg 5: ska Svelte-flytten göras före eller efter att frontendlogiken lyfts ut i vanlig JS?

Avgjorda: fältet `lastUpdate` bytte namn direkt till `measuredAt` (steg 2). Frågan om porten
`127.0.0.1:8080` avgörs i [plan-cicd-2026-11-17.md](plan-cicd-2026-11-17.md) (steg 3: ingen publicerad port).
