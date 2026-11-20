# Plan: "Känns som" – vindkyla med vind från SMHI

Datum: 2026-11-20. **Genomförd 2026-11-20** (se "Genomfört" sist i dokumentet).

## Mål

Varje mätpunkt i listan visar, utöver temperaturen, **"Känns som"** följt av en temperatur där vinden
är inräknad. Sammanfattningen och gubben utgår också från "Känns som". Temperaturen kommer som i dag från temperatur.nu, som fortfarande styr vilka mätpunkter
som finns. Vinden kommer från närmaste vindstation hos SMHI.

- Klienten anropar `/api/search` precis som i dag. Svaret har ett nytt fält med den färdiga
  temperaturen, så klienten räknar ingenting själv.
- Anropet till SMHI görs separat: egen klient, egen `feed.Feed` och eget intervall. Om SMHI är nere
  påverkas inte temperaturerna, och "Känns som" blir samma som temperaturen.
- Vilken vindstation som hör till vilken mätpunkt bestäms **en gång** och sparas i en JSON-fil som
  checkas in. Servern hämtar aldrig stationslistan.
- "Källa: SMHI" står längst ned på sidan, tillsammans med temperatur.nu.

## Kontrollerat i SMHI:s API (2026-11-20)

| Behov | Var i API:t | Kontrollerat |
|---|---|---|
| Stationer med position, för mappningen | `GET /api/version/1.0/parameter/4.json`, fältet `station[]`: `key`, `name`, `latitude`, `longitude`, `height`, `active` | 530 stationer, varav 183 aktiva |
| Senaste vinden för alla stationer i **ett** anrop | `GET /api/version/1.0/parameter/4/station-set/all/period/latest-hour/data.json`, fältet `station[]`: `key`, `value[].value` (m/s som text), `value[].date` (ms, UTC), `value[].quality` | 182 stationer, 49 KB, `Cache-Control: max-age=600`. Alla nycklar finns i stationslistan. |
| Rätt slags vind för formeln | Parameter 4: "Vindhastighet, medelvärde 10 min, 1 gång/tim", mätt på 10 m höjd | Formeln förutsätter vind på 10 m höjd |
| Kvalitet | `quality`: `G` = kontrollerat och godkänt, `Y` = misstänkt eller aggregerat | 171 `G`, 8 `Y` och 3 utan värde. Bara `G` används. |
| Mätpunkterna hos temperatur.nu | `internal/tnumock/stations.json.gz`: `id`, `lat`, `lon` | 1 914 mätpunkter, alla med position och unikt id |
| Hur gammal vinden är | `value[].date` | Mäts varje hel timme och publiceras en stund efter. Vid kontrollen var värdet 37 minuter gammalt. |

Basadress: `https://opendata-download-metobs.smhi.se`. Ingen nyckel krävs, och bara dokumenterade
API:er får användas. Rutnätsanalysen (MESAN, "metanalys") svarade 404 och används inte.

Täckning, beräknad på mätpunkterna i `tnumock` mot de aktiva vindstationerna:

| Största avstånd till vindstationen | Andel mätpunkter som får "Känns som" |
|---|---|
| 20 km | 55 % |
| 30 km | 81 % |
| 50 km | 99 % |

## Vindkylan

SMHI använder sedan vintern 2003/2004 formeln från Osczevski och Bluestein (JAG/TI), uttryckt i m/s:

```
Teff = 13,126667 + 0,6215·T − 13,924748·v^0,16 + 0,4875195·T·v^0,16
```

T är lufttemperaturen i °C och v är vindhastigheten i m/s på 10 m höjd.

**Giltighet enligt SMHI:** formeln används inte vid vind under 1,3 m/s eller över 35 m/s, och inte vid
temperaturer över +10 °C eller under −40 °C.

Exempel: T = −10 °C och v = 10 m/s ger Teff ≈ −20,3 °C. Testerna ska kontrollera formeln mot SMHI:s
publicerade tabell.

**"Känns som" visas alltid** (beslut 2026-11-20). När formeln inte gäller, eller när det inte finns
någon användbar vind, är "Känns som" samma som den uppmätta temperaturen:

| Läge | "Känns som" |
|---|---|
| −40 °C ≤ T ≤ +10 °C och 1,3 ≤ v ≤ 35 m/s | Teff enligt formeln |
| T > +10 °C, T < −40 °C, v < 1,3 m/s eller v > 35 m/s | Samma som T |
| Ingen vindstation inom 30 km, eller inget godkänt vindvärde för stationen | Samma som T |
| Mätpunkten saknar temperatur | Visas inte (`nil`), precis som temperaturen |

## Steg

Varje steg är tänkt som en egen PR.

### Steg 1 – Mappningen (görs en gång)

| Fil | Ändring |
|---|---|
| ny: `cmd/vindmappning/main.go` | Ett verktyg som körs för hand (`go run ./cmd/vindmappning`) och inte ingår i produktionsbinären. Det läser mätpunkterna från `tnumock` och SMHI:s stationslista för parameter 4 (ett enda anrop, eller en sparad fil). För varje mätpunkt väljs den närmaste aktiva vindstationen inom 30 km. Resultatet skrivs sorterat, så att varje körning ger samma fil. |
| ny: `internal/vind/mappning.json` | Checkas in och bäddas in med `go:embed`. Innehåll: när och hur den skapades (datum, parameter, största avstånd) och per mätpunkt (`temperatur.nu`-id) SMHI-stationens nyckel och avståndet i km. Cirka 1 550 rader, runt 80 KB. Mätpunkter utan station inom gränsen finns inte med. |
| ny: `cmd/vindmappning/main_test.go` | Närmaste station väljs, gränsen följs, inaktiva stationer hoppas över och utdatan blir likadan varje gång. |

Mappningen ändras bara när någon kör verktyget igen, t.ex. när `tnumock` uppdateras eller SMHI lägger
ned en station. Nya mätpunkter hos temperatur.nu som saknas i filen får "Känns som" utan vind (samma som temperaturen).

### Steg 2 – SMHI-klienten och en egen bakgrundshämtning

| Fil | Ändring |
|---|---|
| ny: `internal/smhi/client.go` | Hämtar `latest-hour` för parameter 4. Adressen är en konstant, och användarens text används aldrig. Tidsgräns, `LimitReader` (1 MB), `User-Agent` och `httpbody.Drain` vid felstatus. Svaret blir `map[stationsnyckel]Wind{Speed, MeasuredAt}`. Bara `quality == "G"` och värden mellan 0 och 75 m/s tas med; allt annat hoppas över. |
| ny: `internal/smhi/testdata/latest-hour.json` | Ett nedkortat riktigt svar för testerna. |
| ny: `internal/smhi/client_test.go` | Tolkning, kvalitet `Y`, saknade värden, felaktiga tal och felstatus. |
| `internal/feed/feed.go` | Ny metod `Latest() (T, bool)` som läser senaste värdet **utan att vänta**. Då väntar en besökare aldrig på SMHI, inte ens precis efter start. |
| `main.go` | Startar en egen `feed.Feed` för vinden med intervallet 30 minuter (beslut 2026-11-20). Värdena kommer en gång i timmen, och SMHI säger att svaret får cachas i 10 minuter. Det blir 2 anrop i timmen mot SMHI. |

### Steg 3 – Beräkningen

| Fil | Ändring |
|---|---|
| ny: `internal/vind/windchill.go` | `FeelsLike(temp, wind float64) (float64, bool)` med SMHI:s formel och gränserna ovan. Konstanterna har namn. |
| ny: `internal/vind/vind.go` | `Estimator` håller mappningen och vindens `Feed`. `FeelsLike(obs.Station) *float64` slår upp stationen i mappningen, läser vinden med `Latest()`, och räknar ut värdet. Utan användbar vind blir värdet temperaturen. |
| ny: tester | Formeln mot SMHI:s tabell och vid varje gräns, uppslag som saknas och ingen vind alls. Att den senaste vinden ligger kvar när en hämtning misslyckas testas redan i `feed`. |

### Steg 4 – Samma anrop, med ett nytt fält

| Fil | Ändring |
|---|---|
| `internal/server/handlers.go`, `server.go` | `/api/search` lägger till `feelsLike` på varje mätpunkt i svaret, för högst `maxStations` (100) rader, och `averageFeelsLike` för hela platsen (medelvärdet av mätpunkternas `feelsLike`, räknat på samma sätt som `average`). Fältet läggs i en svarstyp i servern, inte i `obs.Station`, eftersom det räknas fram och inte kommer från en källa. `/api/suggest` ändras inte. |
| samma | **Samma cachestrategi som i dag:** `Cache-Control: public, max-age=60` på `/api/search`. Vinden ligger i serverns minne, och ingen besökare kan få servern att anropa SMHI. |
| `internal/server/server_test.go` | `feelsLike` räknas med vind, blir temperaturen utan vind, och sökningen fungerar som vanligt när SMHI är nere. `averageFeelsLike` stämmer med raderna. |

Svaret blir högst cirka 1,6 KB större (cirka 16 byte per rad × 100 rader). Det mäts före och efter.

### Steg 5 – Klienten

| Fil | Ändring |
|---|---|
| `web/app.js` (`stationItem`) | Texten "Känns som" och temperaturen (`formatTemp`) under raden, satt med `textContent`. |
| `web/app.js` (sammanfattningen och gubben) | Sammanfattningen visar "Känns som" under temperaturen. Gubbens kläder och texten om vad man ska ha på sig utgår från "Känns som" (för en mätpunkt: dess `feelsLike`, annars `averageFeelsLike`). Den stora temperaturen är fortfarande den uppmätta. |
| `web/style.css` | Stil för raden med befintliga färger, så att kontrasten (7:1) redan är testad. Om en ny färg behövs ska den in i `contrast_test.go`. Raden ska fungera vid 320 px bredd och 200 % textstorlek. |
| `web/index.html` (sidfoten) | En länk till under temperatur.nu: "Vinddata: Källa: SMHI" som länkar till smhi.se, plus texten "Känns som är beräknad från SMHI:s vindmätningar". Licensen kräver att det står när datan är bearbetad. Bara text, ingen logotyp (SMHI:s bilder omfattas inte av den öppna licensen). |

Skärmbilder i ljust och mörkt läge och i mobilbredd tas före och efter.

### Steg 6 – Dokumentation

| Fil | Ändring |
|---|---|
| `README.md` | Ett nytt avsnitt om SMHI under "API-villkor som följs": CC BY 4.0, "Källa: SMHI" och att bearbetningen anges, bara dokumenterade API:er, ett anrop var 30:e minut och ingen nyckel. Hur mappningen skapas om. |
| `CLAUDE.md` | Arkitekturen: `internal/smhi`, `internal/vind`, `cmd/vindmappning`, och att SMHI ger vind till mätpunkterna, inte egna mätpunkter. |

## Granskning (enligt CLAUDE.md)

- **Säkerhet:** Ingen användartext når SMHI. Svaret begränsas i storlek och valideras. `feelsLike` är
  ett tal och sätts med `textContent`. CSP:n påverkas inte, eftersom anropet görs av servern och
  sidfoten bara har en vanlig länk. Inga nya hemligheter. `govulncheck` körs.
- **API-villkor:** temperatur.nu påverkas inte. Mot SMHI görs ett anrop var 30:e minut, och de båda
  källorna hämtas helt oberoende av varandra.
- **Prestanda:** Uppslaget i mappningen är en map-uppslagning per rad. Benchmarken för `Suggest` ska
  vara oförändrad. Svarsstorlek och minne mäts före och efter.
- **Alltid:** `go vet`, `go test -race`, kontrasttestet och skärmbilder.

## Beslut (2026-11-20)

1. "Känns som" visas alltid. Utan användbar vind är den samma som temperaturen.
2. Vindstationen får ligga högst 30 km från mätpunkten.
3. Vinden hämtas var 30:e minut. Ett lyckat svar ersätter vinden i cachen. Vid fel eller uteblivet svar
   ligger den senaste vinden kvar tills ett nytt svar kommer. Ingen åldersgräns (så fungerar `feed.Feed` redan).
4. Sammanfattningen och gubben använder också "Känns som".
5. Mappningen skapas från mätpunkterna i `tnumock` (från 2026-10-05). Inget anrop mot temperatur.nu
   görs. Mätpunkter som tillkommit senare får "Känns som" utan vind tills kopian uppdateras och
   mappningen skapas om.
6. SMHI kontrolleras var 30:e minut.

## Genomfört

Avvikelser från planen:

- **Namn:** paketen heter `internal/wind` och `cmd/windmap`, och filen `internal/wind/mapping.json`,
  eftersom tekniska namn ska vara på engelska enligt CLAUDE.md.
- **`feelsLike` ligger i `places`:** `places.Search` tar en funktion som räknar ut "Känns som" och lägger
  `feelsLike` i `places.StationResult` (en `obs.Station` plus fältet) och `averageFeelsLike` i
  `SearchResult`. Medelvärdet ska räknas på alla platsens mätpunkter, precis som `average`, och det är
  bara `places` som har alla. `obs.Station` är oförändrad.
- **Mock-läget** (`TNU_MOCK=1`) hämtar vinden från SMHI på riktigt. SMHI har ingen anropskvot och
  ingen nyckel.

Resultat:

- Mappningen: 1 554 av 1 914 mätpunkter (81 %) har en vindstation inom 30 km. Filen är 86 KB, en rad per
  mätpunkt, och blir likadan varje gång verktyget körs med samma indata.
- Formeln stämmer med SMHI:s tabell (testat på sju värden, bl.a. −10 °C och 10 m/s → −20).
- Benchmarken för `Suggest` är oförändrad: 0,19–0,20 ms och 1 384 B (typiskt), 0,33–0,34 ms och 1 688 B
  (värsta fallet).
- `/api/search` blev 7–8 % större: Lund 2 681 → 2 873 B, Uppsala 6 593 → 7 057 B, Kiruna 10 731 → 11 420 B.
