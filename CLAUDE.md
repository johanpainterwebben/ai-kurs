# Vad ska jag ha för kläder?

Go-server + webbsida som visar temperatur från temperatur.nu:s API. Se README.md för hur man kör och vilka API-villkor som följs.

## Granska efter varje större ändring

Efter varje större ändring (ny funktion, ändrad dataflöde, nya endpoints, nya beroenden, större ändringar i webbsidan eller Docker) ska koden granskas för **säkerhet (OWASP Top 10), dåliga mönster och prestanda** innan arbetet rapporteras som klart. Rapportera vad som hittades och åtgärdades, och vad som kontrollerades utan anmärkning.

Minst detta ska kontrolleras:

**Säkerhet / OWASP**
- XSS: data från API:et eller användaren sätts bara med `textContent`/attribut, aldrig `innerHTML`; länkar från API:et valideras (`isTnuURL`).
- CSP och säkerhetsrubriker i `internal/server/server.go` är kvar och blockerar inget som sidan behöver (inga CSP-fel i webbläsarkonsolen).
- Injektion/SSRF: användarens text används aldrig i anrop mot temperatur.nu.
- Hemligheter: `cli`, e-post och `sign` läcker inte till loggar eller felmeddelanden; `.env` och `__debug_bin*` är inte incheckade (`git ls-files`).
- Statiska filer: ingen katalogvisning, ETag/cache fungerar.
- Beroenden: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`.
- Docker: icke-root, skrivskyddat, inga extra rättigheter.

**API-villkoren (temperatur.nu)**
- Samma fråga högst var femte minut – även efter fel, krascher och omstarter.
- Osignerade anrop under ~30 per timme.

**Prestanda**
- Förslag/sök anropas vid varje tangenttryckning och av vem som helst: `go test -run xxx -bench Suggest ./internal/places/` ska ligga kring 0,2–0,4 ms och några KB per anrop även i värsta fallet.
- Ingen onödig vikt i webbsidan (bilder i rätt storlek, cachebara filer).

**Alltid**
- `go vet ./...` och `go test -race ./...`.
- Färgändringar: `contrast_test.go` (WCAG AAA) ska passera.
- Gränssnittsändringar: verifiera med skärmbilder i ljust och mörkt läge och i mobilbredd.
