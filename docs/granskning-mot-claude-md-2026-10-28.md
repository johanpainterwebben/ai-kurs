# Granskning mot arkitekturprinciperna i CLAUDE.md

Datum: 2026-10-28. Ingen kod ändrades. Granskat: `main.go`, `internal/*`, `web/`, `Dockerfile`,
`docker-compose.yml` och workflows i `.github/`. Testerna, `go vet` och benchmarken kördes inte.

Kompletterar [kodanalys-teknisk-skuld-2026-10-28.md](kodanalys-teknisk-skuld-2026-10-28.md) och
[atgardsplan-kodanalys-2026-10-28.md](atgardsplan-kodanalys-2026-10-28.md).

## Där koden bryter mot principerna

### 1. Femminutersregeln och timbudgeten överlever inte en omstart (allvarligast)
- `lastAttempt` i [feed.go](../internal/feed/feed.go) och `calls` i [budget.go](../internal/budget/budget.go) finns bara i minnet.
- Efter krasch eller `restart: unless-stopped` ([docker-compose.yml](../docker-compose.yml)) hämtar `Feed.Run` direkt igen, och budgeten på 25 anrop/timme nollställs.
- CLAUDE.md kräver att villkoren följs "även efter … omstarter". [DEPLOY.md:182](../DEPLOY.md#L182) lägger ansvaret på den som driftar.
- `safeFetch` skyddar bara mot panik under hämtningen, inte mot krasch eller OOM.
- Ingen volym för tillstånd finns, och filsystemet är `read_only`.

### 2. Frontend är inte Svelte
- `web/` är vanlig JavaScript: [app.js](../web/app.js), [index.html](../web/index.html) (med inbäddad SVG) och [style.css](../web/style.css).
- Ingen `package.json`, Vite eller `svelte-check`.
- Filnamnen saknar hash. [static.go:50](../internal/server/static.go#L50) sätter `no-cache` på html/css/js, så de kan inte cachas länge.

### 3. Flera källor och aggregering saknas
- [main.go:98](../main.go#L98) `newSource` returnerar en enda källa.
- `places.NewIndex(src obs.Source, …)` ([index.go:24](../internal/places/index.go#L24)) tar en källa, så det finns ingen `Feed` per källa.
- Inget aggregeringslager (vägning efter avstånd och ålder, extremvärden, spridning).

### 4. Kopplingen till temperatur.nu finns kvar
- [handlers.go:100](../internal/server/handlers.go#L100): feltexten nämner temperatur.nu.
- [server.go:34](../internal/server/server.go#L34): `FetchedAt` kommenterad som "från temperatur.nu".
- [app.js:416](../web/app.js#L416): `isTnuURL` tillåter bara temperatur.nu, så en andra källas länk skulle ersättas.
- [index.html:96](../web/index.html#L96): hårdkodad logotyp.

### 5. Driften avviker från texten
- [docker-compose.yml](../docker-compose.yml) publicerar `127.0.0.1:8080:8080`, men CLAUDE.md säger att ingen port publiceras och att appen bara nås via nätverket `proxy`. Endast loopback, men ändå en avvikelse.

### 6. CI/CD är bara halvvägs
- Inget frontendjobb (bygge, `svelte-check`, lint, `npm audit`); bara en kommentar i [ci.yml](../.github/workflows/ci.yml).
- [publish.yml](../.github/workflows/publish.yml) publicerar imagen, men det finns ingen driftsättning, ingen `/healthz`-kontroll efteråt och ingen rollback.

### 7. Mindre saker
- Cache-tiderna `cacheSearch`/`cacheSuggest` i [server.go](../internal/server/server.go) bygger på att listan förnyas var tionde minut, men `REFRESH_INTERVAL` är konfigurerbar.

## Där koden följer principerna

**Backend och API-villkor**
- [obs.go](../internal/obs/obs.go) är källneutral; `places` och `server` importerar `obs`, inte `tnu`.
- [feed.go](../internal/feed/feed.go): intervall, inget omförsök före ett intervall (även efter fel), delad första hämtning, senast lyckade värde kvar, `Stale` efter flera fel, panikskydd.
- [config.go:48](../internal/config/config.go#L48) höjer `REFRESH_INTERVAL` till minst 5 minuter.
- Osignerade anrop begränsas till 25/timme ([client.go](../internal/tnu/client.go), [budget.go](../internal/budget/budget.go)).
- Besökare läser från cachen; sök och förslag körs lokalt. Användartext når aldrig en extern källa, alltså ingen SSRF-väg.
- Mock ligger utanför produktionsbinären via byggtagg ([mock_on.go](../mock_on.go), [mock_off.go](../mock_off.go)).

**Säkerhet**
- [client.go:100](../internal/tnu/client.go#L100) tar bort URL:en (med `cli`/`sign`) ur felmeddelanden. `.env` och `__debug_bin*` ligger i [.gitignore](../.gitignore); `.env` är inte incheckad.
- [headers.go](../internal/server/headers.go): strikt CSP utan `unsafe-*`, `nosniff`, Referrer-Policy, COOP, Permissions-Policy.
- `query` i [handlers.go:79](../internal/server/handlers.go#L79) avvisar styrtecken och ogiltig UTF-8 och begränsar längden.
- [static.go](../internal/server/static.go): bara kända filer, ingen katalogvisning, ETag på innehåll.
- Timeouts, `MaxHeaderBytes`, ordnad nedstängning och `LimitReader` på svar ([main.go:77](../main.go#L77)).
- [app.js](../web/app.js) använder `textContent`/attribut, inga `innerHTML`-träffar; länkar valideras med `isTnuURL`.

**Docker**
- [Dockerfile](../Dockerfile): distroless, `nonroot`, `CGO_ENABLED=0`, healthcheck via binären.
- [docker-compose.yml](../docker-compose.yml): `read_only`, `cap_drop: [ALL]`, `no-new-privileges`, loggrotation.

**Tillgänglighet**
- [style.css](../web/style.css): `prefers-color-scheme`, `prefers-reduced-motion`, `prefers-contrast`, `:focus-visible`, Atkinson Hyperlegible, storlekar i `rem`.
- [contrast_test.go](../contrast_test.go) kontrollerar 7:1 i båda lägena.
- Förslagslistan har `aria-expanded`, `aria-activedescendant` och `aria-selected`.

**CI**
- [ci.yml](../.github/workflows/ci.yml) och [publish.yml](../.github/workflows/publish.yml): `permissions: {}` med minsta rättigheter per jobb, actions låsta till commit-SHA, `persist-credentials: false`, `pull_request` (inte `_target`), inga hemligheter i PR-flödet.
- CI kör `go vet` (även `-tags mock`), `go test -race`, benchmarken, `govulncheck`, golangci-lint och Trivy.
- Imagen taggas med commit-SHA på GHCR.

## Föreslagen ordning
1. Gör femminutersregeln och budgeten omstartsäkra (tillstånd på en volym). Det är den enda punkten med reell risk att bli blockerad hos temperatur.nu.
2. Gör `places.Index` flerkällsklart: en `Feed` per källa, ett aggregeringslager, källneutrala texter i handlers och frontend.
3. Flytta frontend till Svelte/Vite med hashade filnamn, och lägg till frontendjobbet i CI.
4. Lägg till driftsättning med rollback och ta bort den publicerade porten i compose-filen.
