# Genomförd åtgärdsplan: steg 2, 3, 4, 6 och 7

Datum: 2026-10-28. Beskriver vad som gjordes av [atgardsplan-kodanalys-2026-10-28.md](atgardsplan-kodanalys-2026-10-28.md)
(fynd i [kodanalys-teknisk-skuld-2026-10-28.md](kodanalys-teknisk-skuld-2026-10-28.md)).

**Inte gjort:** steg 1 (beständig femminutersregel och timbudget) och steg 5 (frontend/Svelte), enligt
överenskommelse. Inget är committat.

## Steg 2 – Källneutral modell och riktig mättid (F2, F4)

| Fil | Ändring |
|---|---|
| `internal/obs/obs.go` (ny) | `obs.Station` med `Source`, `MeasuredAt` (`time.Time`, UTC) och `URL`; gränssnittet `obs.Source`; felet `obs.ErrBudgetExhausted`. |
| `internal/tnu/client.go` | Översätter svaren till `obs.Station`. `lastUpdate` tolkas som svensk tid (`Europe/Stockholm`, `time/tzdata` inbäddad eftersom distroless saknar zoneinfo) och blir UTC. `tnu.Station`, `tnu.Source` och `tnu.ErrBudgetExhausted` är borttagna. Ny `DecodeStations`. Kommentar om att MD5 i `sign` är föreskrivet. |
| `internal/places/*`, `internal/server/*` | Använder `obs` i stället för `tnu`; `places` och `server` importerar inte längre `tnu`. |
| `web/app.js` | `lastUpdate` → `measuredAt`; `formatUpdated` visar ISO-tid i svensk tid. |
| `internal/tnu/client_test.go` | Nytt test för sommar-/vintertid och ogiltig tid. |

**API-ändring:** fältet `lastUpdate` heter nu `measuredAt` (ISO, UTC) och varje mätpunkt har `source`.

## Steg 3 – Schemaläggare per källa (F3)

| Fil | Ändring |
|---|---|
| `internal/feed/feed.go` (ny) | Generisk `Feed[T]`: intervall, aldrig tidigare omförsök än ett intervall (även efter fel), delad första hämtning, senast lyckade värdet kvar vid fel, inaktuell efter fler än 3 misslyckanden i rad, panikskydd, `Loaded()`. |
| `internal/feed/feed_test.go` (ny) | De tio garantitesterna från `index_test.go`, flyttade, plus ett test att två källor är oberoende. |
| `internal/budget/` (ny) | `Budget` räknar anrop per tidsfönster, med tester. Placerad separat, inte i feed, eftersom budgeten hör till en källas villkor. |
| `internal/tnu/client.go` | Använder `budget.Budget` (bara osignerat). |
| `internal/places/index.go` | `Index` är en tunn omslagsklass kring `Feed[*Snapshot]`; ny metod `Ready()`. |
| `internal/places/index_test.go` | Skrivet om till två små tester; schemaläggningen testas i `feed`. |

Sammanslagning av flera källor ingår inte; den byggs när en andra källa läggs till.

## Steg 4 – Uppdelning av `match.go` (F5–F8)

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

## Steg 6 – Server och `main` (F12, F13)

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

## Steg 7 – Hygien, Docker och CI (F14, F16–F20)

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

`docker-compose.yml` är oförändrad: porten `127.0.0.1:8080` är bara nåbar från servern själv, vilket
stämmer med "ingen port mot internet" i CLAUDE.md, och DEPLOY.md:s curl-steg förutsätter den.

## Verifiering

- `go vet ./...` och `go vet -tags mock ./...`: rent.
- `go test ./...`: alla paket gröna.
- `staticcheck`: inga fynd. `govulncheck` (v1.9.0): inga sårbarheter.
- Rökprov av servern i mock-läge: `/healthz`, `/api/search`, `/api/suggest`, 400-svar, rubriker och
  `temperatur healthcheck` (exit 0 vid frisk server, 1 vid fel).
- `git ls-files`: ingen `.env` eller `__debug_bin*` incheckad. `cli`/`sign` loggas inte.

## Inte verifierat

- `go test -race`: kräver C-kompilator och kunde inte köras lokalt; körs i CI.
- Docker-bygget, `golangci-lint` och workflow-filerna är inte körda. Första CI-körningen kan ge
  småanmärkningar.
- Driftsättning med återställning är inte automatiserad (kräver val mellan self-hosted runner och en
  tjänst på servern); beskrivet i DEPLOY.md.

## Kvar

1. **Steg 1:** beständig femminutersregel och timbudget över omstarter. `Feed` och `Budget` är nu varsitt
   ställe att spara tillstånd från.
2. **Steg 5:** frontend (ren logik, temperaturtabell, delade konstanter, Svelte).
3. Automatiserad driftsättning, och pinning av basimagernas digest.
