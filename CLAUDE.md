# Vad ska jag ha för kläder?

## Syfte

Ta reda på vilken temperatur det är där man är eller dit man ska, så att man vet vad man ska ha på sig. Sidan visar temperaturen och en gubbe som är klädd efter den.

I dag finns en enda temperaturkälla (temperatur.nu). Målet är att använda flera källor där det går och slå ihop dem till ett samlat resultat för platsen.

Se README.md för hur man kör projektet och vilka API-villkor som följs, och DEPLOY.md för hur servern sätts upp.

## Arkitektur

```
Besökare → Cloudflare → Cloudflare Tunnel → nginx (egen container) → app (Go, egen container) → cache → källor (temperatur.nu, …)
```

### Backend – Go

- `main.go` väljer källa (`newSource`), startar bakgrundsuppdateringen och HTTP-servern och stänger ned snyggt. `temperatur healthcheck` anropar `/healthz` och används av Docker `HEALTHCHECK` (imagen saknar curl).
- `internal/config/` läser och kontrollerar miljövariablerna (`config.Load`).
- `internal/obs/` är den källneutrala modellen: `obs.Station` (med `Source`, `MeasuredAt` som `time.Time` i UTC och källans `URL`), gränssnittet `obs.Source` och felet `obs.ErrBudgetExhausted`. Resten av programmet känner bara till `obs`, aldrig till en enskild källas typer.
- `internal/tnu/` är klienten mot temperatur.nu. Den sköter signering och översätter svaren till `obs.Station`. `internal/budget/` räknar anrop per tidsfönster (osignerade anrop: högst 25/timme).
- `internal/feed/` hämtar data från en källa i bakgrunden (`feed.Feed[T]`): intervall, aldrig tidigare omförsök än ett intervall (femminutersregeln, även efter fel), en delad första hämtning, senast lyckade värdet kvar vid fel, inaktuell-flagga efter flera misslyckanden i rad och panikskydd. Varje källa får en egen `Feed`.
- `internal/places/` bygger sökindexet av mätpunkterna (`snapshot.go`), normaliserar namn (`fold.go`, `data.go`), rankar (`rank.go`) och matchar feltolerant (`matcher.go`, `distance.go`). `index.go` kopplar sökindexet till en `feed.Feed`. Besökare läser alltid från cachen och väntar aldrig på en extern källa. Sök, förslag och felstavning körs lokalt mot cachen och kostar inga API-anrop.
- `internal/server/` innehåller endpoints (`handlers.go`: `/api/search`, `/api/suggest`, `/healthz`), statiska filer (`static.go`) och säkerhetsrubriker med CSP (`headers.go`).
- `internal/tnumock/` har sparade svar från temperatur.nu för utveckling och tester. Den importeras bara av `mock_on.go` (byggtagg `mock`), så `TNU_MOCK=1` fungerar bara med `go run -tags mock .` och finns inte i produktionsbinären.
- Frontendfilerna bäddas in i binären med `go:embed`, så det finns bara en binär att driftsätta.

### Namn i koden

Domänbegrepp som kommer från temperatur.nu och svenska orter heter som i svenskan (`kommun`, `ort`, `plats`, `ortName`, `kommunNames`). Allt annat – tekniska begrepp, typer och funktioner (`Feed`, `Snapshot`, `Budget`, `respond`) – har engelska namn. Kommentarer och felmeddelanden är på svenska.

### Flera källor (målbild)

- Varje källa är ett eget paket under `internal/` som implementerar ett gemensamt gränssnitt (`obs.Source`, jfr `tnu.Client`) och körs i en egen `feed.Feed`. Källan följer sina egna API-villkor, med eget uppdateringsintervall, egen anropsbudget och egen signering.
- Varje källa har en egen cache och hämtas i bakgrunden. Om en källa är nere eller långsam ska de andra inte påverkas, och då visas senast kända data, markerade som gamla.
- Ett eget lager slår ihop källorna per plats till ett samlat resultat. Det vägs efter avstånd och ålder på mätningen, och extremvärden sorteras bort. Svaret visar vilka källor som ingår och hur mycket de skiljer sig åt.
- Varje källa ska ha källhänvisning i gränssnittet om dess villkor kräver det.

### Lätt lista, detaljer vid utfällning (målbild)

Listan över mätpunkter ska vara så lätt som möjligt, och mer data hämtas först när man fäller ut en enskild rad. Källorna har ingen egen vy; det är samma lista oavsett källa.

- **I listan** visas bara det som behövs för en snabb överblick: namn, temperatur och tid, och eventuellt någon enstaka uppgift till (t.ex. vind). Varje fält som läggs till i `/api/search` multipliceras med upp till `maxStations` (100) rader för varje sökning, så nya fält i listan ska motiveras.
- **Vid utfällning** hämtar sidan detaljerna för just den mätpunkten, och inte förrän dess. Det kräver en egen endpoint (t.ex. `/api/station`) som tar mätpunktens `source` och `id`, aldrig fri text, och som validerar dem mot cachen.
- **Detaljerna kommer från serverns cache.** Besökare får aldrig utlösa ett anrop mot en källa. Behöver en källa ett eget anrop per mätpunkt går det genom källans `budget`, har en egen cache med minst fem minuters giltighet och körs i bakgrunden eller delas mellan besökare, så att femminutersregeln och anropsgränsen följs även när många fäller ut samma rad.
- **Datamodellen** (`obs.Station`) delas upp i en lätt del som alltid finns i listan och en detaljdel med valfria fält (`nil` när källan saknar dem, som `Temp` i dag). Dagens `moh`, `sourceInfo`, `forutsattning`, `uptime` och `homepageUrl` från temperatur.nu läses inte in än och är de första kandidaterna för detaljdelen.
- **Tillgänglighet:** utfällningen är en riktig knapp med `aria-expanded` och `aria-controls`, fungerar med tangentbord, har synlig fokusmarkering och ingen rörelse vid `prefers-reduced-motion`. Raden är i dag en länk till temperatur.nu, så länken och knappen får skilda ytor.
- **Prestanda:** mät svarsstorlek för `/api/search` och minne före och efter, och håll benchmarken för `Suggest` oförändrad.

### Frontend – Svelte

- Frontend ska skrivas i **Svelte** (Svelte 5 + Vite). I dag är den skriven i vanlig JavaScript i `web/`, och den ska flyttas över till Svelte.
- Frontend byggs till statiska filer som Go-servern bäddar in och serverar. I produktion körs ingen Node-server. Filnamnen ska innehålla en hash så att filerna kan cachas länge.
- Kodbasen ska klara den strikta CSP:n utan `unsafe-inline`/`unsafe-eval`. Var försiktig med Svelte-funktioner som lägger in egna stilmallar när sidan körs (t.ex. `transition:`/`animate:`) och kontrollera dem mot CSP:n.
- Frontend ska följa **WCAG 2.2 nivå AAA** där det är möjligt:
  - Kontrast minst 7:1 för text, eller 4,5:1 för stor text, i både ljust och mörkt läge.
  - Allt ska gå att använda med tangentbord och ha synlig fokusmarkering.
  - Korrekt semantik och ARIA, så att skärmläsare fungerar (förslagslistan är en combobox).
  - Respektera `prefers-reduced-motion` och `prefers-color-scheme`.
  - Texten ska gå att förstora till 200 % och fungera vid 320 px bredd utan vågrät rullning.
  - Lättläst typsnitt (Atkinson Hyperlegible) och enkel svenska.

### Drift

- **Debian Linux-server** med Docker.
- Appen körs i en egen container som icke-root, med skrivskyddat filsystem och utan extra rättigheter. Den publicerar ingen port mot internet utan nås bara via Docker-nätverket `proxy`.
- **nginx** körs i en egen container och tar emot trafiken från **Cloudflare Tunnel** (`cloudflared`). Servern har inga öppna inkommande portar. Konfigurationen för nginx och tunneln ligger i repot `~/Projects/server`.
- Hemligheter (`TNU_CLI`, `TNU_CLI_KEY` och nycklar för framtida källor) ligger i `.env` på servern och ska aldrig checkas in.

### CI/CD

Verktyget är **GitHub Actions**. `.github/workflows/ci.yml` (Go-kontroller, `golangci-lint`, Docker-bygge med Trivy) och `publish.yml` (bygger och publicerar imagen på GHCR) finns redan. Det som återstår är frontend-stegen (väntar på Svelte) och automatisk driftsättning med återställning; se DEPLOY.md. Målbilden:

- **Vid varje PR och push:**
  - Go: `go vet ./...`, `go test -race ./...`, benchmarken för `Suggest`, `govulncheck` och `golangci-lint` (`.golangci.yml`; ett rött lint-steg blockerar).
  - Frontend: bygge, `svelte-check`, lint och `npm audit`.
  - Kontrasttestet.
  - Docker-bygge med sårbarhetsskanning av imagen (t.ex. Trivy).
- **Vid merge till `main`:** imagen byggs, taggas med commit-SHA och publiceras på GHCR. Därefter driftsätts den på servern. Eftersom servern inte har några öppna portar ska driftsättningen hämtas därifrån: antingen en self-hosted runner som bara kör jobb från `main` (aldrig kod från PR:er eller forkar), eller en tjänst på servern som hämtar nya imagar. Efter driftsättningen kontrolleras `/healthz`, och om den inte svarar rullas den tillbaka till föregående image.
- Workflows ska ha minsta möjliga `permissions`, actions ska vara låsta till commit-SHA och hemligheter ska ligga i GitHub Secrets/Environments.

## Principer

- **Säkerhet (OWASP Top 10) är viktigt.** Det gäller både backend, frontend, Docker och CI/CD.
- **Prestanda är viktigt.** Svar till besökare kommer alltid från cachen på servern, och sidan ska vara lätt och cachebar.
- **Tillgänglighet:** WCAG på högsta nivå (AAA) för läsbarhet och kontrast.
- **Läsbar kod före kommentarer.** Koden ska gå att förstå utan kommentarer: tydliga namn, små funktioner som gör en sak och namngivna konstanter i stället för magiska tal. Kommentera inte det koden redan säger. Skriv bara en kommentar när den förklarar *varför* (ett API-villkor, en säkerhetsregel, ett medvetet val eller en inte uppenbar invariant). Vill du skriva en kommentar för att förklara vad koden gör, döp om eller bryt ut en funktion i stället. Doc-kommentarer på exporterade namn och paket behålls.
- **API-villkor** för varje källa följs alltid, även efter fel, krascher och omstarter.

## Granska efter varje större ändring

Efter varje större ändring (ny funktion, ändrat dataflöde, nya endpoints, nya källor, nya beroenden, större ändringar i webbsidan, Docker eller CI/CD) ska koden granskas för **säkerhet (OWASP Top 10), dåliga mönster och prestanda** innan arbetet rapporteras som klart. Rapportera vad som hittades och åtgärdades, och vad som kontrollerades utan anmärkning.

Minst detta ska kontrolleras:

**Säkerhet / OWASP**
- XSS: data från API:et eller användaren sätts bara med `textContent`/attribut, aldrig `innerHTML` (i Svelte: aldrig `{@html}` med sådan data). Länkar från API:et valideras (`isTnuURL`).
- CSP och säkerhetsrubriker i `internal/server/headers.go` är kvar och blockerar inget som sidan behöver (inga CSP-fel i webbläsarkonsolen).
- Injektion/SSRF: användarens text används aldrig i anrop mot temperatur.nu eller andra källor.
- Hemligheter: `cli`, e-post, nycklar och `sign` läcker inte till loggar, felmeddelanden eller CI-loggar. `.env` och `__debug_bin*` är inte incheckade (`git ls-files`).
- Statiska filer: ingen katalogvisning, ETag/cache fungerar.
- Beroenden: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (och `npm audit` för frontend).
- Docker: icke-root, skrivskyddat, inga extra rättigheter.
- CI/CD: minsta möjliga `permissions`, inga hemligheter tillgängliga för kod från PR:er eller forkar.

**API-villkoren (temperatur.nu och övriga källor)**
- Samma fråga högst var femte minut – även efter fel, krascher och omstarter.
- Osignerade anrop under ~30 per timme.
- Nya källor: deras egna villkor är dokumenterade i README.md och följs.

**Prestanda**
- Förslag/sök anropas vid varje tangenttryckning och av vem som helst: `go test -run xxx -bench Suggest ./internal/places/` ska ligga kring 0,2–0,4 ms och några KB per anrop även i värsta fallet.
- Ingen onödig vikt i webbsidan (bilder i rätt storlek, cachebara filer, liten JS-bundle).

**Alltid**
- `go vet ./...` och `go test -race ./...`.
- Färgändringar: `contrast_test.go` (WCAG AAA) ska passera.
- Gränssnittsändringar: verifiera med skärmbilder i ljust och mörkt läge och i mobilbredd.
