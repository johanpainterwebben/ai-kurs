# Vad ska jag ha för kläder?

Sök en ort och se aktuell temperatur från mätpunkterna där, hur kallt det känns när vinden räknas in – och en gubbe som visar vad man ska ha på sig. Temperaturer från [temperatur.nu](https://www.temperatur.nu/), vind från [SMHI](https://www.smhi.se/).

## Köra lokalt

Med sparade svar (förbrukar ingen anropskvot; kräver `-tags mock`, som aldrig används i produktionsbygget):

```
TNU_MOCK=1 go run -tags mock .
```

Mot det riktiga API:et: kopiera `.env.example` till `.env`, fyll i `TNU_CLI` och kör
`set -a; . ./.env; set +a; go run .`

Öppna http://localhost:8080. I VS Code finns båda varianterna under Run and Debug (F5).

Tester: `go test ./...` (inklusive en kontroll av att färgerna i `web/style.css` klarar WCAG AAA-kontrast i ljust och mörkt läge)

## API-villkor som följs

### temperatur.nu

Se https://www.temperatur.nu/info/api/.

- **Samma fråga högst var femte minut:** servern hämtar alla mätpunkter i ett enda anrop när den startar och förnyar sedan listan i bakgrunden var 10:e minut (`REFRESH_INTERVAL`, minst 5m). Besökare läser alltid från cachen och väntar aldrig på temperatur.nu. Misslyckade försök görs inte om förrän intervallet passerat.
- **Sök, förslag och felstavning** körs lokalt mot listan och kostar inga API-anrop.
- **Osignerade anrop:** utan `TNU_CLI_KEY` tillåter servern sig högst 25 anrop per timme (i praktiken blir det 6).
- **Signering:** med `TNU_CLI_KEY` signeras varje anrop (`sign` sist i URL:en).
- **Källhänvisning:** länk och logotyp visas i sidfoten och varje mätpunkt länkar till temperatur.nu.

### SMHI (vind för "Känns som")

Öppna data från SMHI:s meteorologiska observationer, under licensen CC BY 4.0. Se https://www.smhi.se/data/om-smhis-data/villkor-for-anvandning och https://opendata.smhi.se/apidocs/metobs.

- **Ett anrop var 30:e minut:** servern hämtar senaste timmens vindhastighet (parameter 4) för alla stationer i ett enda anrop, i bakgrunden och oberoende av temperatur.nu. SMHI mäter en gång i timmen. Om ett anrop misslyckas ligger den senaste vinden kvar tills nästa lyckade. Ingen nyckel krävs.
- **Bara dokumenterade API:er** används, och bara värden med kvaliteten "kontrollerat och godkänt" (`G`).
- **Källhänvisning och bearbetning:** sidfoten säger "Vinddata: Källa: SMHI" och att "Känns som" är beräknad från SMHI:s vindmätningar.
- **Vindkylan** räknas med samma formel som SMHI använder (Osczevski och Bluestein, SMHI:s faktablad nr 17). Utanför formelns gränser (över +10 °C, under −40 °C, vind under 1,3 eller över 35 m/s), eller utan vind, är "Känns som" samma som temperaturen.

**Vilken vindstation som hör till varje mätpunkt** står i `internal/wind/mapping.json`: den närmaste aktiva stationen inom 30 km. Filen skapas en gång och checkas in, och servern hämtar aldrig stationslistan. Mätpunkterna tas från de sparade svaren i `internal/tnumock`, så inget anrop görs mot temperatur.nu. Skapa om den när de sparade svaren uppdateras eller SMHI lägger ned stationer:

```
go run ./cmd/windmap
```

## Docker

```
docker compose up -d --build
```

Kräver `.env` (se ovan). Lyssnar på 127.0.0.1:8080 och ansluter till ett delat Docker-nätverk, där en gemensam omvänd proxy och tunnel (i ett separat serverrepo) publicerar den. Starta dem först.

Hur servern sätts upp beskrivs i [DEPLOY.md](DEPLOY.md).
