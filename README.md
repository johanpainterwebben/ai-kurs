# Vad ska jag ha för kläder?

[![CI/CD](https://github.com/johanpainterwebben/ai-kurs/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/johanpainterwebben/ai-kurs/actions/workflows/ci.yml)

Sök på en ort och se temperaturen just nu, hur kallt det känns när vinden räknas in och vad du ska ha på dig.

**Live:** https://temperatur.pilros.se

## Vad det gör

Du söker på en kommun, ort eller plats i Sverige och får temperaturen från mätpunkterna där, både för varje mätpunkt och som ett snitt för platsen. Temperaturerna kommer från temperatur.nu, som har cirka 1 900 mätpunkter i hela landet: i bostadsområden, i tätorter och längs vägar. Du får därför temperaturen nära där du är eller ska, i stället för ett enda värde för en större ort eller region, som vädertjänster med färre stationer ger (SMHI har till exempel cirka 230 stationer som mäter temperaturen). Varje temperatur visas också som "Känns som", där vinden från närmaste vindstation hos SMHI är inräknad, och en gubbe visar vilka kläder som passar. Medan du skriver föreslår sidan platser, och den klarar felstavningar som "upsala".

## Kom igång

### Krav

- Go 1.26 eller senare.
- För att köra mot det riktiga API:et hos temperatur.nu: en e-postadress (`TNU_CLI`) och helst en klientnyckel (`TNU_CLI_KEY`) från [temperatur.nu](https://www.temperatur.nu/info/api/). Utan dem går projektet att köra med sparade svar.
- SMHI:s vinddata kräver ingen nyckel.
- Docker med Compose v2, om du vill köra det som i produktion.

### Installation

```
git clone https://github.com/johanpainterwebben/ai-kurs
cd ai-kurs
```

Det finns inga beroenden att installera, eftersom projektet bara använder Go:s standardbibliotek.

### Konfiguration

Skapa en `.env`-fil i projektets rot:

```
TNU_CLI=din@epost.se
TNU_CLI_KEY=din-klientnyckel
```

Alla variabler, även de valfria `REFRESH_INTERVAL` och `ADDR`, finns listade i `.env.example`. `.env` får aldrig checkas in.

### Kör projektet

Med sparade svar från temperatur.nu (ingen nyckel behövs, och inga anrop görs mot temperatur.nu):

```
TNU_MOCK=1 go run -tags mock .
```

Mot det riktiga API:et:

```
set -a; . ./.env; set +a; go run .
```

Öppna http://localhost:8080. I VS Code finns båda varianterna under Run and Debug (F5).

Tester, inklusive en kontroll av att färgerna klarar WCAG AAA-kontrast i ljust och mörkt läge, och benchmarken för sökförslagen:

```
go test ./...
go test -run xxx -bench Suggest ./internal/places/
```

I Docker byggs och körs projektet lokalt med `docker compose -f docker-compose.yml -f compose.build.yml up -d --build`. Compose-filen förutsätter ett delat Docker-nätverk från driftmiljön. Hur servern sätts upp beskrivs i [DEPLOY.md](DEPLOY.md).

## Arkitektur

Allt körs i en enda Go-binär, som också serverar webbsidan (filerna är inbäddade i binären). Servern hämtar all data i bakgrunden: mätpunkterna från temperatur.nu var 10:e minut och vinden från SMHI var 30:e minut. Datan ligger i en cache i minnet. Besökarnas sökningar och förslag körs bara mot cachen och gör aldrig själva anrop mot källorna, vilket följer API-villkoren och gör svaren snabba. En fast mappning kopplar varje mätpunkt till närmaste vindstation, och "Känns som" räknas ut på servern och skickas färdigt till klienten. Klienten är vanlig HTML, CSS och JavaScript som bara visar det servern skickar. I drift körs containern bakom en omvänd proxy och en utgående tunnel, så servern har inga öppna portar.

```
ai-kurs/
├── main.go             # startar källorna, bakgrundshämtningen och HTTP-servern
├── healthcheck.go      # "temperatur healthcheck" för Dockers HEALTHCHECK
├── cmd/windmap/        # verktyg som skapar mappningen mätpunkt → vindstation
├── internal/
│   ├── config/         # läser och kontrollerar miljövariablerna
│   ├── obs/            # källneutral modell för mätpunkter
│   ├── tnu/            # klient mot temperatur.nu (signering, anropsgräns)
│   ├── smhi/           # klient mot SMHI:s vinddata
│   ├── feed/           # bakgrundshämtning och cache, en per källa
│   ├── budget/         # räknar anrop per tidsfönster
│   ├── wind/           # "Känns som" och mappningen till vindstationer
│   ├── places/         # sökindex, förslag och felstavning
│   ├── server/         # API-endpoints, statiska filer och säkerhetsrubriker
│   ├── httpbody/       # hjälpfunktion för HTTP-svar
│   └── tnumock/        # sparade svar för utveckling och tester
├── web/                # webbsidan: HTML, CSS, JavaScript och typsnitt
├── docs/               # planer och granskningar
├── .github/            # CI/CD: tester, lint, sårbarhetsskanning, publicering och signering av imagen
├── deploy/             # skript och systemd-timer som driftsätter nya versioner på servern
├── Dockerfile          # bygger en minimal image som kör som icke-root
├── docker-compose.yml  # kör den publicerade imagen skrivskyddad och utan extra rättigheter
├── compose.build.yml   # bygger imagen lokalt i stället
├── DEPLOY.md           # hur servern sätts upp
└── CLAUDE.md           # principer och instruktioner för den agentdrivna utvecklingen
```

## Teknisk stack

| Komponent | Val | Anledning |
|---|---|---|
| Backend | Go, bara standardbiblioteket | En statisk binär med webbsidan inbäddad. Inga externa beroenden att hålla uppdaterade. |
| Frontend | HTML, CSS och JavaScript utan ramverk | En liten sida som klarar en strikt CSP utan `unsafe-inline`. En flytt till Svelte är planerad. |
| Temperatur | temperatur.nu:s API | Cirka 1 900 mätpunkter i hela Sverige i ett enda anrop. |
| Vind | SMHI:s öppna data (metobs) | Kontrollerade mätningar, gratis och utan nyckel. Ger "Känns som" med SMHI:s egen formel för vindkyla. |
| Cache | I minnet i Go-processen | Datan är liten och ändras långsamt, så ingen databas behövs. |
| Container | Docker, distroless-image | Kör som icke-root, med skrivskyddat filsystem och utan extra rättigheter. |
| Ingång | Omvänd proxy och utgående tunnel | Servern har inga öppna inkommande portar. |
| CI/CD | GitHub Actions, GHCR, cosign och en systemd-timer på servern | Tester, `golangci-lint`, `govulncheck` och Trivy vid varje push och pull request. På `main` publiceras och signeras imagen, och servern hämtar och startar den själv, med automatisk återställning. Inga hemligheter i GitHub behövs. |
| Tillgänglighet | WCAG 2.2 AAA, typsnittet Atkinson Hyperlegible | Kontrasten testas automatiskt, och sidan fungerar med tangentbord, skärmläsare och vid 320 px bredd. |

## Begränsningar

- **Bara en temperaturkälla.** Allt kommer från temperatur.nu. Att slå ihop flera källor per plats är en målbild men inte gjort.
- **"Känns som" är en uppskattning.** Vinden kommer från närmaste SMHI-station inom 30 km och är mätt på 10 m höjd i öppen terräng, inte vid mätpunkten. 81 % av mätpunkterna har en vindstation inom 30 km. För övriga, och när formeln inte gäller (över +10 °C), är "Känns som" samma som temperaturen.
- **Vinden kan vara gammal.** SMHI mäter en gång i timmen. Om SMHI är nere används den senaste vinden utan åldersgräns, och sidan säger inte att den är gammal.
- **Mappningen till vindstationer är fast.** Den bygger på sparade mätpunkter från 2026-10-05. Mätpunkter som har tillkommit senare får ingen vind förrän mappningen skapas om.
- **En omstart gör ett nytt anrop.** Femminutersregeln mot temperatur.nu sparas inte på disk, så varje omstart, även vid driftsättning, hämtar listan på nytt.
- **Driftsättningen tar ett par minuter.** Servern frågar efter nya versioner en gång i minuten i stället för att få dem skickade, eftersom den inte har några öppna portar.
- **Filnamnen saknar hash.** Efter en ny version kan en CDN framför servern ge besökare gammal JavaScript och CSS i upp till fyra timmar.
- **Datan från temperatur.nu kontrolleras inte i alla delar.** Orimliga värden (t.ex. 999 °C eller texten "NaN") används som de är.
- **Appen begränsar inte antalet anrop per besökare.** Det skyddet förutsätts ligga framför servern.
- **Bara Sverige och bara aktuella värden.** Det finns ingen prognos och ingen historik.

## API-villkor som följs

### temperatur.nu

Se https://www.temperatur.nu/info/api/.

- **Samma fråga högst var femte minut:** servern hämtar alla mätpunkter i ett enda anrop när den startar och förnyar sedan listan i bakgrunden var 10:e minut (`REFRESH_INTERVAL`, minst 5m). Besökare läser alltid från cachen och väntar aldrig på temperatur.nu. Misslyckade försök görs inte om förrän intervallet har passerat.
- **Sök, förslag och felstavning** körs lokalt mot listan och kostar inga API-anrop.
- **Osignerade anrop:** utan `TNU_CLI_KEY` tillåter servern sig högst 25 anrop per timme (i praktiken blir det 6).
- **Signering:** med `TNU_CLI_KEY` signeras varje anrop (`sign` sist i URL:en).
- **Källhänvisning:** länk och logotyp visas i sidfoten, och varje mätpunkt länkar till temperatur.nu.

### SMHI (vind för "Känns som")

Öppna data från SMHI:s meteorologiska observationer, under licensen CC BY 4.0. Se https://www.smhi.se/data/om-smhis-data/villkor-for-anvandning och https://opendata.smhi.se/apidocs/metobs.

- **Ett anrop var 30:e minut:** servern hämtar senaste timmens vindhastighet (parameter 4) för alla stationer i ett enda anrop, i bakgrunden och oberoende av temperatur.nu. Om ett anrop misslyckas ligger den senaste vinden kvar tills nästa lyckade. Ingen nyckel krävs.
- **Bara dokumenterade API:er** används, och bara värden med kvaliteten "kontrollerat och godkänt" (`G`).
- **Källhänvisning och bearbetning:** sidfoten säger "Vinddata: Källa: SMHI" och att "Känns som" är beräknad från SMHI:s vindmätningar.
- **Vindkylan** räknas med samma formel som SMHI använder (Osczevski och Bluestein, SMHI:s faktablad nr 17). Utanför formelns gränser (över +10 °C, under −40 °C, vind under 1,3 eller över 35 m/s), eller utan vind, är "Känns som" samma som temperaturen.

**Vilken vindstation som hör till varje mätpunkt** står i `internal/wind/mapping.json`: den närmaste aktiva stationen inom 30 km. Filen skapas en gång och checkas in, och servern hämtar aldrig stationslistan. Mätpunkterna tas från de sparade svaren i `internal/tnumock`, så inget anrop görs mot temperatur.nu. Skapa om filen när de sparade svaren uppdateras eller SMHI lägger ned stationer:

```
go run ./cmd/windmap
```

## Utvecklat med

Claude Code (agentdriven utveckling) som en del av kursen Next-Generation Software Development with AI.

## Sammanfattning

Designade och utvecklade en webbtjänst som visar temperaturen och hur kallt det känns där man är eller ska, med Go, vanlig HTML, CSS och JavaScript, Docker bakom nginx och Cloudflare Tunnel på en egen Linux-server och GitHub Actions, med hjälp av agentdriven utveckling (Claude Code). Projektet löser problemet att veta vad man ska ha på sig, genom att slå ihop lokala temperaturer från temperatur.nu med vind från SMHI, och är tillgängligt på https://temperatur.pilros.se.
