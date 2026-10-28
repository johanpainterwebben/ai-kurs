# Säkerhetsgranskning: hemligheter, indata, injektion, loggar och standardvärden

Datum: 2026-10-28. Ingen kod ändrades. Granskat: all Go-kod utanför tester (`main.go`, `healthcheck.go`,
`mock_*.go`, `internal/*`), `web/app.js` och `web/index.html`, `Dockerfile`, `docker-compose.yml`,
`.dockerignore`, `.gitignore`, `.golangci.yml`, `.github/`, `.vscode/launch.json`, `.env.example`, `DEPLOY.md`,
`README.md`. Git-historiken söktes efter incheckade hemligheter. Testerna, `go vet`, `govulncheck` och
`npm audit` kördes inte (frontend saknar ännu `package.json`).

## Sammanfattning

Koden är i god ordning på det mesta som efterfrågades. Det finns **inga hårdkodade hemligheter, ingen SQL,
inget kommandoanrop och ingen filsökväg som byggs av användarens text**. De fynd som finns är lågt till medelhögt
allvarliga och handlar mest om skydd i flera led (defense in depth) och driftens standardvärden.

| Nr | Fynd | Allvar |
|---|---|---|
| F1 | Publiceringen av imagen väntar inte på att CI lyckas | Medel |
| F2 | Ingen begränsning av antalet anrop per besökare på `/api/*` | Medel |
| F3 | API-data (URL, texter) skickas vidare utan validering på servern | Låg–medel |
| F4 | `/healthz` avslöjar internt tillstånd för alla | Låg |
| F5 | `ADDR` har ingen kontroll och standardvärdet lyssnar på alla gränssnitt | Låg |
| F6 | `TNU_CLI` kontrolleras bara för att vara icke-tom | Låg |
| F7 | `.gitignore` och `.dockerignore` täcker för lite (bl.a. `temperatur.exe` på Windows) | Låg |
| F8 | Basimagerna i `Dockerfile` är inte låsta till digest | Låg |
| F9 | `docker-compose.yml` saknar resursgränser; porten 8080 publiceras på värden | Låg |
| F10 | Säkerhetsrubriker saknas: HSTS m.fl. sätts inte av appen | Låg |
| F11 | Anropsbudgeten och femminutersregeln nollställs vid omstart | Låg (API-villkor) |
| F12 | Panikmeddelandet loggas med `%v` | Info |
| F13 | `cli` är en e-postadress som skickas till tredje part | Info |
| F14 | Personuppgifter och interna uppgifter i dokumentationen | Info |

---

## Fynd

### F1. Publiceringen väntar inte på CI (Medel)
- [.github/workflows/publish.yml:6-8](../.github/workflows/publish.yml#L6-L8): `publish` triggas av `push` till `main`,
  helt oberoende av `ci.yml`. En image kan alltså publiceras på GHCR även om `go test -race`, `govulncheck` eller
  `golangci-lint` misslyckas i samma push.
- Publiceringen kör bara Trivy ([publish.yml:38-44](../.github/workflows/publish.yml#L38-L44)), inte tester eller `govulncheck`.
- Imagen signeras inte och har ingen provenance/SBOM ([publish.yml:31-37](../.github/workflows/publish.yml#L31-L37)),
  så servern kan inte kontrollera att imagen kommer från just det här bygget.
- **Åtgärd:** låt `publish` köras via `workflow_run` på lyckad CI, eller gör `ci.yml` till ett återanvändbart workflow
  som `publish` anropar med `needs:`. Skydda `main` med obligatoriska statuskontroller. Överväg `cosign` eller
  `provenance: true`/`sbom: true` i `docker/build-push-action`.

### F2. Ingen begränsning av anrop per besökare (Medel)
- [internal/server/server.go:66-73](../internal/server/server.go#L66-L73): ingen rate limiting, inga gränser för samtidiga anrop.
  `/api/suggest` och `/api/search` kör en fuzzy-matchning (Levenshtein, se `internal/places/matcher.go:125-147`)
  mot hela listan för varje anrop, och kostar alltså CPU. Frågan är begränsad till 50 tecken
  ([server.go:16-17](../internal/server/server.go#L16-L17)), vilket håller varje anrop billigt (benchmark 0,2–0,4 ms),
  men många anrop från en och samma klient kan ändå belasta servern.
- Skyddet finns i dag bara i Cloudflare (se [DEPLOY.md:111-115](../DEPLOY.md#L111-L115)) och är en rekommendation,
  inte något appen eller repot garanterar. En som når `nginx` direkt på Docker-nätverket eller via
  `127.0.0.1:8080` ([docker-compose.yml:6-9](../docker-compose.yml#L6-L9)) går förbi det.
- **Åtgärd:** lägg `limit_req` i nginx-konfigurationen (repot `~/Projects/server`) och/eller en enkel token bucket per IP i
  appen. Dokumentera det i `DEPLOY.md`.

### F3. Data från API:et skickas vidare utan validering på servern (Låg–medel)
- [internal/tnu/client.go:158-169](../internal/tnu/client.go#L158-L169): `URL`, `Title`, `Kommun`, `Lan` och `ID` tas rakt av
  från temperatur.nu och skickas till webbläsaren. Servern kontrollerar varken schema/värdnamn på `URL` eller längd
  på texterna.
- Frontend skyddar med `isTnuURL` ([web/app.js:416-423](../web/app.js#L416-L423)) och `textContent`, och
  CSP:n blockerar inbäddad kod ([internal/server/headers.go:7-8](../internal/server/headers.go#L7-L8)), så
  det går **inte** att utnyttja i dag. Men skyddet ligger enbart i klienten: en framtida Svelte-komponent, eller en annan
  klient av API:et, som glömmer kontrollen får en `javascript:`-URL rakt in i en `href`.
- Fler källor planeras (CLAUDE.md, "Flera källor"). Då multipliceras antalet ställen där ovaliderad data kommer in.
- **Åtgärd:** validera i `tnu.apiResponse.stations()` (eller i ett gemensamt steg i `obs`): tillåt bara `https://` mot
  den egna källans värdnamn, kapa längder, avvisa styrtecken i `Title`/`Kommun`/`Lan`, och kontrollera att `Temp`,
  `Lat` och `Lon` ligger i rimliga intervall (Lat −90…90, Lon −180…180, Temp t.ex. −90…70). Behåll klientens kontroll
  som andra lager.
- [internal/tnu/client.go:147-153](../internal/tnu/client.go#L147-L153) (`DecodeStations`) läser utan storleksgräns. Den
  används bara av mocken med inbäddad fil, så risken är teoretisk, men en `io.LimitReader` som i
  [client.go:104](../internal/tnu/client.go#L104) gör funktionen säker om den återanvänds.

### F4. `/healthz` avslöjar internt tillstånd (Låg)
- [internal/server/handlers.go:52-63](../internal/server/handlers.go#L52-L63): svaret säger `ok: väntar på första hämtningen`
  eller `ok: listan kan vara inaktuell`. Det nås av vem som helst via Cloudflare (inget i repot begränsar
  `/healthz` till interna anrop). En angripare ser när källan har problem, eller att appen just startats om, och
  kan välja tidpunkt för sina försök. Samma information finns redan i `stale` i `/api/search`
  ([handlers.go:28](../internal/server/handlers.go#L28)), så effekten är liten.
- **Åtgärd:** svara bara `ok` utåt, och blockera `/healthz` i nginx för trafik utifrån (Docker-`HEALTHCHECK` går
  direkt mot appen). Loggen har redan detaljerna.

### F5. `ADDR` kontrolleras inte och standardvärdet är öppet (Låg)
- [internal/config/config.go:53-55](../internal/config/config.go#L53-L55): `ADDR` används som den är, utan kontroll.
  Ett fel visar sig först vid `ListenAndServe`.
- [config.go:24](../internal/config/config.go#L24): standardvärdet `:8080` lyssnar på **alla** gränssnitt. I containern är det
  rätt, men vid `go run .` på en utvecklardator blir tjänsten nåbar från hela det lokala nätverket (README anger
  `localhost:8080`, [README.md:16](../README.md#L16)).
- **Åtgärd:** kontrollera värdet med `net.SplitHostPort`. Använd `127.0.0.1:8080` som standard utanför containern
  och sätt `ADDR=:8080` uttryckligen i `docker-compose.yml`. (`healthcheck.go:18-20` hanterar redan tomt värdnamn.)

### F6. `TNU_CLI` kontrolleras bara för att vara icke-tom (Låg)
- [internal/config/config.go:56-58](../internal/config/config.go#L56-L58) och [.env.example:3](../.env.example#L3):
  placeholdern `din@epost.se` accepteras som giltigt värde, och det går att starta produktion med ett värde som inte är en
  e-postadress. API-villkoren kräver en riktig adress när anropen är osignerade, och utan en kan klienten blockeras
  ([client.go:41-42](../internal/tnu/client.go#L41-L42)).
- Värdet escapas korrekt i URL:en ([client.go:115](../internal/tnu/client.go#L115): `url.QueryEscape`), så det finns ingen
  injektionsrisk, bara en konfigurationsrisk.
- `TNU_CLI_KEY` trimmas inte ([config.go:42](../internal/config/config.go#L42)). Ett blanksteg eller radbrytning sist i `.env`
  ger en felaktig signatur som ser ut som ett API-fel.
- **Åtgärd:** `strings.TrimSpace` på båda, `net/mail.ParseAddress` på `TNU_CLI` när nyckel saknas, och avvisa
  `din@epost.se`.

### F7. `.gitignore` och `.dockerignore` täcker för lite (Låg)
- [.gitignore:1-3](../.gitignore#L1-L3) ignorerar bara `.env`, `/temperatur` och `__debug_bin*`. Utvecklaren kör Windows, där
  `go build` ger `temperatur.exe`, som inte matchas. Saknas gör också `.env.*` (t.ex. `.env.local`, `.env.production`),
  `tunnel.env` ([DEPLOY.md:99](../DEPLOY.md#L99) nämner filen), `*.pem` och `*.key`.
- [.dockerignore:1-5](../.dockerignore#L1-L5) har samma brist, och `COPY . .` ([Dockerfile:6](../Dockerfile#L6)) tar med allt som
  inte undantas, bl.a. `docs/`, `.github/` och eventuella `.env.*`. Det hamnar bara i bygglagret (slutimagen
  innehåller bara binären), men kan läcka via bygg-cachen eller en `--target build`-image.
- **Kontrollerat utan anmärkning:** `git ls-files` visar varken `.env` eller `__debug_bin*`, och
  `git log --all -- .env` ger inga träffar. Ingen hemlighet har checkats in.
- **Åtgärd:** lägg till `*.exe`, `.env.*` (med `!.env.example`), `*.pem`, `*.key`, `tunnel.env` i båda filerna, och
  `docs/`, `.github/`, `*.md` i `.dockerignore`.

### F8. Basimagerna är inte låsta (Låg)
- [Dockerfile:1](../Dockerfile#L1) (`golang:1.26-alpine`) och [Dockerfile:11](../Dockerfile#L11) (`gcr.io/distroless/static:nonroot`)
  refererar till föränderliga taggar. Byggen är inte reproducerbara, och en komprometterad tagg får genomslag direkt.
  Actions är däremot låsta till commit-SHA, så imagerna avviker från resten av CI/CD-hållningen.
- Dependabot ([.github/dependabot.yml:12-15](../.github/dependabot.yml#L12-L15)) kan uppdatera digest-låsta imagar.
- **Åtgärd:** lås med `@sha256:…` och låt Dependabot föreslå uppdateringar.

### F9. Resursgränser saknas; porten publiceras på värden (Låg)
- [docker-compose.yml:12-15](../docker-compose.yml#L12-L15) har `read_only`, `cap_drop: [ALL]` och `no-new-privileges`, vilket är bra.
  Det saknas `mem_limit`, `pids_limit` och `cpus`, så en minnesläcka eller överbelastning kan dra ned hela värden
  (där nginx och cloudflared också körs). `tmpfs: /tmp` kan behövas om något senare skriver till disk.
- [docker-compose.yml:6-9](../docker-compose.yml#L6-L9) publicerar `127.0.0.1:8080`. Det är bara åtkomligt lokalt, men
  CLAUDE.md säger att appen *"inte publicerar någon port mot internet och bara nås via nätverket `proxy`"*. Varje lokal
  användare på servern, eller en annan container med `network_mode: host`, går förbi nginx och Cloudflare (och därmed F2).
- `env_file: .env` ([docker-compose.yml:5](../docker-compose.yml#L5)) lägger `TNU_CLI_KEY` i containerns miljö, där den syns
  för den som kör `docker inspect`. Det är normalt, men en reda på att Docker-åtkomst därför motsvarar åtkomst till nyckeln
  (och att kontot inte ska ligga i gruppen `docker`, vilket [DEPLOY.md:58](../DEPLOY.md#L58) redan påpekar).
- **Åtgärd:** lägg till `mem_limit`, `pids_limit`, `cpus`. Flytta felsökningsporten till en
  `docker-compose.override.yml` som inte används i produktion.

### F10. Säkerhetsrubriker: HSTS m.fl. sätts inte av appen (Låg)
- [internal/server/headers.go:12-18](../internal/server/headers.go#L12-L18) sätter CSP, `nosniff`, `Referrer-Policy`, COOP och
  `Permissions-Policy`. Saknas gör `Strict-Transport-Security` och `Cross-Origin-Resource-Policy: same-origin`.
  [DEPLOY.md:148](../DEPLOY.md#L148) förväntar sig att HSTS finns (`grep strict-transport`), men inget i det här
  repot sätter den. Det måste då göras av Cloudflare eller nginx; det går inte att verifiera härifrån.
- CSP i sig är stram (`default-src 'self'`, `object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'none'`) och utan
  `unsafe-inline`/`unsafe-eval`. **Kontrollerat utan anmärkning.**
- **Åtgärd:** bekräfta att HSTS sätts i nginx/Cloudflare (`curl -sI`), annars lägg den i `securityHeaders`.

### F11. Anropsbudgeten och femminutersregeln nollställs vid omstart (Låg, men API-villkor)
- [internal/budget/budget.go:16-18](../internal/budget/budget.go#L16-L18) och [internal/feed/feed.go:37-41](../internal/feed/feed.go#L37-L41)
  håller allt i minnet. Varje omstart ger ett nytt anrop direkt (`lastAttempt.IsZero()`, [feed.go:124](../internal/feed/feed.go#L124)) och en
  full timbudget ([client.go:61-63](../internal/tnu/client.go#L61-L63)).
- [docker-compose.yml:4](../docker-compose.yml#L4) har `restart: unless-stopped`. Om processen kraschar direkt efter start
  (t.ex. fel konfiguration som ändå passerar `config.Load`) blir det en omstartsloop som bryter mot villkoret
  *"samma fråga högst var femte minut, även efter krascher och omstarter"* och kan få klienten blockerad.
  [feed.go:163-166](../internal/feed/feed.go#L163-L166) fångar paniker i hämtningen, men inte krascher i övrigt.
- Detta är redan kartlagt som allvarligast i [granskning-mot-claude-md-2026-10-28.md](granskning-mot-claude-md-2026-10-28.md);
  upprepas här eftersom det är ett *osäkert standardbeteende*. Åtgärd där: spara tidpunkten för senaste anrop på disk
  (kräver en skrivbar volym, då `read_only: true` annars hindrar det) och vänta ut resten av intervallet vid start.

### F12. Panikmeddelandet loggas med `%v` (Info)
- [internal/feed/feed.go:171](../internal/feed/feed.go#L171): `fmt.Errorf("… %v", f.name, r)` loggar vad som än orsakade paniken. En
  index-panik innehåller bara index och längd, men en panik med ett värde ur API-svaret kan i teorin få med data från
  källan i loggen. Ingen hemlighet är inblandad, eftersom `cli` och nyckeln aldrig passerar den vägen.
- Ingen stackspårning loggas, vilket gör en orsaksanalys svår. Vid behov: logga `debug.Stack()` på `Debug`-nivå.

### F13. `cli` är en e-postadress som skickas till tredje part (Info)
- [internal/tnu/client.go:115](../internal/tnu/client.go#L115): `cli=<e-post>` ingår i varje URL till api.temperatur.nu. Det följer
  API-villkoren, men innebär att adressen (en personuppgift) finns i leverantörens loggar och i alla mellanled som
  loggar URL:er. Inget av det kan appen styra.
- Appen själv skyddar värdet bra: `url.Error` packas upp så att URL:en aldrig hamnar i felmeddelanden
  ([client.go:90-97](../internal/tnu/client.go#L90-L97)), och det finns ett test som kontrollerar det
  (`client_test.go:111-118`). **Kontrollerat utan anmärkning.**
- **Åtgärd:** använd en funktionsadress (t.ex. `temperatur@…`) i stället för en personlig e-postadress, och skaffa
  signeringsnyckeln före lansering, vilket DEPLOY.md redan rekommenderar ([DEPLOY.md:22](../DEPLOY.md#L22)).

### F14. Personuppgifter och interna uppgifter i dokumentationen (Info)
- [DEPLOY.md:31](../DEPLOY.md#L31) och [DEPLOY.md:58](../DEPLOY.md#L58) (användarnamnet `johan`), [DEPLOY.md:92](../DEPLOY.md#L92)
  (GitHub-kontot `johanpainterwebben/ai-kurs`), [DEPLOY.md:3](../DEPLOY.md#L3) och [README.md:36](../README.md#L36) (domänen
  `pilros.se`) samt serverns katalogstruktur (`~/Projects/server`, nätverket `proxy`). Inget av det är en hemlighet och
  domänen är offentlig, men uppgifterna underlättar riktade försök om repot blir publikt. Repot är privat i dag
  ([DEPLOY.md:86](../DEPLOY.md#L86)).
- **Åtgärd:** ingen krävs så länge repot är privat. Byt till platshållare om det publiceras.

---

## Kontrollerat utan anmärkning

| Område | Resultat | Var |
|---|---|---|
| **Hårdkodade hemligheter** | Inga. Sökning efter `password`, `secret`, `token`, `api_key`, privata nycklar och e-postadresser gav bara platshållare (`din@epost.se`, `min@epost.se`, `hemlig` i tester) | hela repot |
| **Hemligheter i git** | `.env` har aldrig checkats in; `.env.example` har tomt `TNU_CLI_KEY` | `git log --all`, `.env.example:5` |
| **SQL-injektion** | Ingen databas eller SQL finns | – |
| **Kommandoinjektion** | Inga anrop till `os/exec`, skal eller `syscall.Exec` | – |
| **Path traversal** | Statiska filer serveras bara om sökvägen finns i en förberäknad lista (`etags`); allt annat ger 404, ingen katalogvisning. Användarens text används aldrig i en filsökväg | `internal/server/static.go:44-52` |
| **SSRF / injektion mot källan** | Användarens `q` används bara mot den lokala cachen. URL:en mot temperatur.nu byggs av konstanter och `TNU_CLI`; `endpoint` är hårdkodad | `client.go:35,114-120`, `handlers.go:23-35` |
| **Inputvalidering `q`** | Längd 2–50 tecken (räknat i tecken, inte byte), styrtecken och ogiltig UTF-8 avvisas, blanksteg normaliseras. Fuzzy-matchningen är begränsad av frågelängden | `handlers.go:68-75` |
| **HTTP-metoder och routes** | Routerna är begränsade till `GET`; övriga metoder ger 405 | `server.go:68-71` |
| **Serverns tidsgränser** | `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` och `MaxHeaderBytes` (16 kB) är satta, vilket skyddar mot Slowloris | `main.go:76-84` |
| **Gränser mot källan** | Svaret begränsas till 16 MB, 30 s timeout, felsvar läses högst 64 kB | `client.go:58,101,104` |
| **Känsliga data i loggar** | Loggen innehåller bara antal, intervall, adress och felkoder. `cli`, nyckel och `sign` loggas aldrig; `Config.CLIKey` har kommentaren "får aldrig loggas" och skrivs inte ut | `main.go:88,109-111`, `client.go:90-97`, `config.go:31` |
| **Besökarens data i loggar** | Inga IP-adresser, sökfrågor eller `User-Agent` loggas | `handlers.go:107,115` |
| **XSS** | Allt från API:et och användaren sätts med `textContent`, `append` eller attribut; `innerHTML`, `eval` och `document.write` används inte. Länkar valideras med `isTnuURL`. Externa länkar har `rel="noopener"`. | `web/app.js` (hela), `index.html:82,94` |
| **Felmeddelanden till besökare** | Generiska svenska texter, inga interna fel eller stackspårningar | `handlers.go:93-100` |
| **Mock i produktion** | Utan `-tags mock` ger `TNU_MOCK=1` ett startfel, och Dockerfile bygger utan taggen | `mock_off.go:14-16`, `Dockerfile:8` |
| **Container** | Icke-root (`nonroot`), distroless utan skal, skrivskyddat filsystem, `cap_drop: ALL`, `no-new-privileges`, loggrotation | `Dockerfile:11,14`, `docker-compose.yml:12-18` |
| **CI/CD** | `permissions: {}` på toppnivå och minsta möjliga per jobb, actions låsta till commit-SHA, `persist-credentials: false`, inga hemligheter för PR:er, `pull_request` (inte `_target`), bara `GITHUB_TOKEN` | `ci.yml:10,21-26`, `publish.yml:10,20-26,45-49` |
| **Injektion i workflow** | Inga `${{ github.event.* }}` i `run:`-steg; `GITHUB_REPOSITORY` är betrodd | `publish.yml:29` |
| **MD5** | Föreskrivet av API:ets signering, avsiktligt och dokumenterat (`nolint`) | `client.go:14,125-127` |
| **Lintning** | `gosec`, `staticcheck`, `errcheck` och `bodyclose` är aktiverade | `.golangci.yml:5-12` |
| **Beroenden** | `go.mod` saknar externa beroenden (inga `require`), så ingen yta från tredjepartspaket | `go.mod` |

## Rekommenderad ordning

1. **F1** (publicera bara efter lyckad CI) och **F2** (rate limiting) – de enda som påverkar säkerheten på riktigt.
2. **F3** (validera källdata) innan fler källor läggs till.
3. **F7**, **F5**, **F6**, **F4** – små ändringar på några rader.
4. **F8–F10** när driftsättningen automatiseras.
5. **F11** hänger ihop med åtgärdsplanen i [atgardsplan-kodanalys-2026-10-28.md](atgardsplan-kodanalys-2026-10-28.md).
