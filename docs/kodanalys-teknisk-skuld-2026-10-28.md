# Kodanalys: läsbarhet, duplicerad logik och teknisk skuld

Datum: 2026-10-28. Ingen kod ändrades; detta är en analys. Åtgärdsplanen (filer, ändringar och
ordning) finns i [atgardsplan-kodanalys-2026-10-28.md](atgardsplan-kodanalys-2026-10-28.md).

## Sammanfattning

Kodbasen är liten (ca 2 500 rader kod) och i gott skick: tydliga kommentarer, bra tester av
`Index` med falsk klocka, och säkerhetsrubriker, CSP och läckageskydd på plats. Analysen letar
efter det som kommer att kosta när målbilden i CLAUDE.md förverkligas: **flera källor**,
**Svelte-frontend** och **CI/CD**.

Granskat: `main.go`, `internal/tnu/*`, `internal/places/*`, `internal/server/*`, `web/app.js`,
`contrast_test.go`, `Dockerfile`, `docker-compose.yml`, README/CLAUDE.md.

## Fynd, i prioritetsordning

### Hög – risk mot API-villkor eller blockerar målbilden

1. **Femminutersregeln och timbudgeten överlever inte en omstart.**
   `lastAttempt` ([index.go:36](../internal/places/index.go#L36)) och `calls` i tnu-klienten
   ([client.go:68](../internal/tnu/client.go#L68)) ligger bara i minnet. Efter krasch, OOM eller
   `restart: unless-stopped` hämtar servern direkt igen, och budgeten på 25 anrop/timme nollställs.
   CLAUDE.md kräver uttryckligen "även efter … omstarter". Panikskyddet i `fetch` täcker bara
   panik under hämtning, inte en krasch någon annanstans. Filsystemet är `read_only`, så det
   behövs en volym eller ett tmpfs-undantag för en liten tillståndsfil (senaste försök,
   anropslogg).

2. **Domänmodellen är temperatur.nu:s modell.** `tnu.Station` används rakt igenom `places`
   (index, snapshot, sök), `server` (JSON) och frontend. `places` importerar `tnu`, och `server`
   importerar `tnu` bara för `tnu.ErrBudgetExhausted`
   ([server.go:131](../internal/server/server.go#L131)). En andra källa kräver en källneutral
   `Observation`/`Station` (id, källa, namn, position, temperatur, mättid som `time.Time`,
   källhänvisning) och ett gemensamt fel för "budget slut".

3. **Hastighetsbegränsningen är uppdelad på två paket.** Femminutersregeln bor i `places.Index`,
   timbudgeten i `tnu.Client`, och kopplingen mellan dem finns bara som kommentar
   ([client.go:8](../internal/tnu/client.go#L8): "places.Index är den enda som anropar
   klienten"). Med flera källor dupliceras det. Mål: en generell per-källa-schemaläggare
   (intervall, budget, backoff, persistens, stale-räknare) som `Index` använder, medan `Index`
   behåller snapshot-bygget.

4. **`LastUpdate` är en sträng i svensk lokal tid**
   ([client.go:52](../internal/tnu/client.go#L52)), och JS jämför den som text mot dagens datum
   ([app.js:438](../web/app.js#L438)). Sammanslagningen ska vägas efter mätningens ålder; då måste
   den vara en `time.Time` (parsad med `Europe/Stockholm`) och gå ut som ISO-UTC. `Lat`/`Lon`
   hämtas men används inte än (avståndsvägning kommer att behöva dem).

### Medel – läsbarhet och dubbelarbete

5. **`match.go` (506 rader) gör fyra saker:** bygga snapshot (`newSnapshot`, ~100 rader med tre
   kartor, closures och ordningsberoende steg: sammanslagningen plats→ort måste ske före
   `places` byggs), normalisering (`fold`, alias), ranking och feltolerant matchning. Dela i
   `snapshot.go`, `fold.go`, `rank.go`/`matcher.go`. Dela upp `newSnapshot` i namngivna steg
   (gruppera, slå ihop, bestäm `Detail`, sortera).

6. **Poängskalan blandar tre betydelser i ett heltal**
   ([match.go:425](../internal/places/match.go#L425)): `scoreContains + n` (stavfel) och
   `scoreViaOrt = 3` överlappar (en prefixträff via ort får samma poäng som en träff med ett
   stavfel). `fuzzy` härleds med `score > scoreContains` på två ställen. Gör det till en struct
   (`kind`, `typos`, `viaOrt`) med en `less`-funktion.

7. **Magiska strängar för `Kind`** ("kommun"/"ort"/"plats") i Go, JSON och JS
   ([app.js:150](../web/app.js#L150)). En typad konstant i Go och en gemensam konstant i
   frontend.

8. **Data i kod:** `irregularKommuner` (en post) och `aliases` (två poster) växer med tiden.
   Flytta till en liten datafil eller tabell med test per post.

9. **Dubbla gränser mellan Go och JS:** minsta/högsta söklängd finns som `minQueryLen`/
   `maxQueryLen` och i felmeddelandet "Skriv mellan 2 och 50 tecken"
   ([server.go:78](../internal/server/server.go#L78)), samt som `length < 2` på tre ställen i
   app.js. Bygg meddelandet av konstanterna; i Svelte en delad konstant.

10. **Två temperaturskalor som måste hållas lika för hand.** `tempLevel` (−10/0/10/20/25) och
    `OUTFITS` (−12/−4/4/10/15/20/25/30/35) i app.js, plus 29 CSS-regler för
    `temp-*`/`t-*`/`data-climate`. Texten "dunjacka, halsduk, mössa, vantar och kängor" står två
    gånger. Gör skalan till data (en tabell) och härled klass och text ur den.

11. **`app.js`: global muterbar state och blandade ansvar.** 451 rader. Vissa element cachas
    överst, andra slås upp med `getElementById` i `render()` (summary-link, summary-title m.fl.).
    `render()` blandar DOM, logik och skärmläsartext. Reservlänken
    `"https://www.temperatur.nu/"` finns på två ställen. Inga JS-tester. Svelte-flytten är rätt
    tillfälle: lyft ut ren logik (`tempLevel`, outfits, formatterare, `isTnuURL`) till testbara
    moduler först.

12. **`server.go` samlar flera ansvar:** routing, validering, statisk filservering, rubriker och
    felmappning. `handleSearch`/`handleSuggest` upprepar mönstret query → snapshot →
    cache-rubrik → JSON. `Cache-Control`-värden är magiska tal. `writeJSON` och `/healthz`
    ignorerar skrivfel; `/healthz` säger ingenting om listan (aldrig hämtad / inaktuell). Dela
    upp i filer (`static.go`, `headers.go`, `handlers.go`).

13. **`main.go:run` (77 rader)** gör konfigläsning, källval, uppstart och nedstängning utan en
    konfigstruktur och utan tester av env-tolkningen. Med fler källor växer `if TNU_MOCK`-grenen.
    Inför `config.Load()` och en `buildSources(cfg)`.

### Låg – kosmetik och hygien

14. **Mock-data i produktionsbinären:** `internal/tnu/mock.go` bäddar in `stations.json.gz`, och
    `TNU_MOCK=1` kan sättas i produktion av misstag. Flytta bakom byggtagg eller till ett
    testpaket.
15. **Test av färger via regex mot CSS** ([contrast_test.go:103](../contrast_test.go#L103) letar
    efter `:root {` och `\n}`) och ligger i paketet `main`. Går sönder vid omformaterad CSS och
    kommer att kräva omskrivning när Svelte/Vite hashar och delar CSS. Flytta kontrasten till en
    tokens-fil (JSON/JS) som både CSS-bygget och testet läser.
16. **Tester som bygger på verklig mock-data** (`realSnapshot`): kan ändras när mock-filen
    uppdateras. Behåll ett fåtal, lägg till små handgjorda fixturer för reglerna.
17. **Språkblandning:** engelska identifierare (`Index`, `Snapshot`) tillsammans med svenska
    (`ortName`, `kommunNames`, `byTempDesc`). Välj en konvention och dokumentera den i CLAUDE.md.
18. **Docker/drift:** `COPY . .` före `go build` ger ingen cache för moduler; ingen
    `HEALTHCHECK`; basimage ej pinnad med digest. `docker-compose.yml` publicerar
    `127.0.0.1:8080`, medan CLAUDE.md säger att ingen port publiceras.
19. **MD5 i `sign`:** föreskrivet av API:et, men en kommentar som säger det förhindrar att en
    linter eller framtida granskare "åtgärdar" det.
20. **CI/CD, linter och beroendekontroller saknas** (känt). Lägg in `golangci-lint` med
    `staticcheck`, `errcheck`, `gosec` när workflows upprättas.

## Kontrollerat utan anmärkning

- XSS: all API-data sätts via `textContent`/attribut; `isTnuURL` valideras före `href`.
- CSP, `nosniff`, `Referrer-Policy`, ingen katalogvisning (filer utanför `etags` ger 404).
- Fel från `http.Client` strippas från URL (ingen `cli`/`sign` i loggar), med test.
- Användartext går aldrig till källan; sök och förslag körs lokalt mot cachen.
- Samtidighet i `Index` (en delad hämtning, mutex, stängd kanal) är korrekt och väl testad.
- `prefixDistance` är allokeringsfri och har ett tidigt avbrott; passar benchmarkmålet.
- Docker: icke-root (distroless:nonroot), `read_only`, `cap_drop: ALL`, `no-new-privileges`.
