# Vad ska jag ha för kläder?

Sök en ort och se aktuell temperatur från mätpunkterna där – och en gubbe som visar vad man ska ha på sig. Data från [temperatur.nu](https://www.temperatur.nu/).

## Köra lokalt

Med sparade svar (förbrukar ingen anropskvot):

```
TNU_MOCK=1 go run .
```

Mot det riktiga API:et: kopiera `.env.example` till `.env`, fyll i `TNU_CLI` och kör
`set -a; . ./.env; set +a; go run .`

Öppna http://localhost:8080. I VS Code finns båda varianterna under Run and Debug (F5).

Tester: `go test ./...` (inklusive en kontroll av att färgerna i `web/style.css` klarar WCAG AAA-kontrast i ljust och mörkt läge)

## API-villkor som följs

Se https://www.temperatur.nu/info/api/.

- **Samma fråga högst var femte minut:** servern hämtar alla mätpunkter i ett enda anrop när den startar och förnyar sedan listan i bakgrunden var 10:e minut (`REFRESH_INTERVAL`, minst 5m). Besökare läser alltid från cachen och väntar aldrig på temperatur.nu. Misslyckade försök görs inte om förrän intervallet passerat.
- **Sök, förslag och felstavning** körs lokalt mot listan och kostar inga API-anrop.
- **Osignerade anrop:** utan `TNU_CLI_KEY` tillåter servern sig högst 25 anrop per timme (i praktiken blir det 6).
- **Signering:** med `TNU_CLI_KEY` signeras varje anrop (`sign` sist i URL:en).
- **Källhänvisning:** länk och logotyp visas i sidfoten och varje mätpunkt länkar till temperatur.nu.

## Docker

```
docker compose up -d --build
```

Lyssnar på 127.0.0.1:8080. Lägg en reverse proxy med HTTPS framför (krävs senare för "min plats").
