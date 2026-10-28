# Kodkvalitet: de fem allvarligaste problemen

Datum: 2026-10-28. Granskat: `main.go`, `internal/tnu`, `internal/feed`, `internal/places`, `internal/server`, `internal/config`, `healthcheck.go` och `web/app.js`.

Granskningen letade efter fyra saker:

1. funktioner som gör mer än en sak
2. otydlig namngivning
3. saknad felhantering vid externa anrop
4. kod som är svår att läsa utan kommentarer

Problemen är rankade efter hur mycket de kan skada. Det är ingen fullständig lista.

## Sammanfattning

| # | Problem | Kategori | Fil |
|---|---------|----------|-----|
| 1 | Ett tomt eller trasigt svar från temperatur.nu ersätter god cache | Felhantering, externt anrop | `internal/tnu/client.go`, `internal/places/index.go` |
| 2 | Frontend tolkar svaret före statuskontroll och saknar timeout | Felhantering, externt anrop | `web/app.js` |
| 3 | `render()` gör åtta saker i en funktion | Gör mer än en sak | `web/app.js` |
| 4 | Gruppering av platser: dolda biverkningar, krypterade namn | Gör mer än en sak, namngivning | `internal/places/snapshot.go` |
| 5 | Rangordningen kräver kommentarer för att kunna läsas | Svårläst, namngivning | `internal/places/matcher.go`, `rank.go` |

---

## 1. Ett tomt eller trasigt svar ersätter den goda cachen

**Kategori:** saknad felhantering för externt anrop.
**Plats:** [client.go:80-111](../internal/tnu/client.go#L80-L111), [client.go:155-172](../internal/tnu/client.go#L155-L172), [index.go:25-33](../internal/places/index.go#L25-L33)

**Problem.** `get` kontrollerar statuskod, JSON-syntax och `client == "blocked"`. Den kontrollerar inte att innehållet är rimligt. Om API:et svarar `200 OK` med `{"stations": []}`, med ett ändrat fältnamn eller med ett felobjekt, räknas hämtningen som lyckad.

Då händer detta:
- `AllStations` returnerar en tom lista utan fel.
- `newSnapshot` bygger ett tomt index.
- `feed.Feed` ersätter det senast lyckade värdet och nollställer felräknaren.

Resultat: alla sökningar ger "Hittade ingen ort" under minst ett intervall (10 minuter). `Stale` blir aldrig `true`, eftersom hämtningen räknades som lyckad. Det är precis det fall som feed-paketets "senast lyckade värdet kvar vid fel" ska skydda mot.

Det finns fler brister i samma kodstig:
- Kontrollen `Client == "blocked"` sker först efter avkodningen. Ett `200` med HTML-felsida ger "kunde inte tolka svaret" i stället för `ErrBlocked`.
- Statuskoden `429` hanteras som alla andra fel, trots att den borde loggas tydligt eftersom den hänger ihop med API-villkoren.
- Stationer utan `ID` eller `Title` släpps igenom.

**Förslag.** Låt `AllStations` returnera ett fel om svaret saknar stationer, till exempel `ErrEmptyResponse`. Ännu bättre är ett fel när antalet sjunker under en bråkdel av förra hämtningen, men det kräver mer logik. Flytta `blocked`-kontrollen så att den görs även om `stations` saknas.

---

## 2. Frontend tolkar svaret före statuskontrollen och saknar timeout

**Kategori:** saknad felhantering för externt anrop.
**Plats:** [app.js:63-81](../web/app.js#L63-L81), [app.js:322-343](../web/app.js#L322-L343)

**Problem.** I `search` körs `await res.json()` på rad 71, före `if (!res.ok)` på rad 72. Backend svarar alltid med JSON. Men nginx, Cloudflare och Docker svarar med HTML vid `502`, `503` och `504`. Då kastar `res.json()` ett `SyntaxError`. `catch` visar texten "Kunde inte nå servern", fast servern nåddes och svarade med ett fel. Det är missvisande för besökaren och för den som felsöker.

Fler brister:
- Anropet har ingen timeout. Ett hängande anrop visar "Söker…" tills webbläsaren ger upp. Då vet inte besökaren om något händer.
- `fetchSuggestions` sväljer alla fel med en tom `catch` och en kommentar. Det är ett medvetet val för förslag, men `!res.ok` på rad 332 returnerar tyst utan att nollställa tidigare förslag. Förslagslistan kan då visa data för en annan fråga än den som skrivits.
- `data.didYouMean` och `data.stations` används utan kontroll att de finns. Ett svar med fel form ger `TypeError`, som `catch` felaktigt tolkar som nätverksfel.

**Förslag.** Kontrollera `res.ok` och `Content-Type` före `res.json()`. Använd `AbortSignal.timeout(10000)` ihop med den befintliga kontrollanten, till exempel via `AbortSignal.any`. Skilj på tre fall med olika meddelanden: ingen kontakt, serverfel och oväntat svar.

---

## 3. `render()` gör åtta saker i en funktion

**Kategori:** funktion som gör mer än en sak.
**Plats:** [app.js:83-143](../web/app.js#L83-L143)

**Problem.** `render` är 60 rader och ansvarar för allt detta:
1. Visar "ingen träff" och avgör om träffen är tvetydig, med en lokal `sameName`-funktion.
2. Skriver in platsens namn i söktextfältet (`input.value = place.query`).
3. Skriver kortets rubrik och detaljtext.
4. Visar eller döljer varningen om gammal data.
5. Grenar mellan en mätpunkt och flera: olika `meta`, olika länkar, olika listinnehåll.
6. Sätter färgnivå på kortet.
7. Klär gubben via `showWeather`.
8. Bygger skärmläsartexten.

Funktionen hämtar dessutom sju element med `document.getElementById` inne i sig. Fem andra element är globala konstanter överst i filen. Det är inkonsekvent och gör att man inte ser vilka element funktionen rör vid.

Samma sorts uppdelning görs på två ställen med olika detaljnivå: gubbens temperatur beräknas på rad 133 (`shownTemp`) och skärmläsartexten på rad 139–141. De bygger på samma villkor (`total === 1`), men uttrycks på olika sätt.

Namn som `temp` (rad 139) betyder en formaterad sträng. På andra ställen betyder `temp` ett tal eller ett DOM-element (rad 188). `summary` betyder både ett DOM-element (`summaryEl`) och en sträng (rad 140).

**Förslag.** Dela upp i `renderNoMatch(data)`, `renderSummary(data)`, `renderStations(data)` och `announce(data)`. Beräkna `shownTemp` en gång. Samla elementreferenserna överst. Byt `temp` mot `tempText` där det är en sträng.

---

## 4. Gruppering av platser: dolda biverkningar och krypterade namn

**Kategori:** funktioner som gör mer än en sak, otydlig namngivning.
**Plats:** [snapshot.go:80-190](../internal/places/snapshot.go#L80-L190)

**Problem.**

*Dolda biverkningar.* `ortPlaces()` och `platsPlaces()` ser ut som funktioner som läser och returnerar. Men båda ändrar de delade `*place`-objekten i kartorna:
- `ortPlaces` sätter `p.Detail` och kan hoppa över orten (`continue`).
- `platsPlaces` lägger till i `p.names` och sätter `p.Detail`.
- Båda returnerar sedan en kopia (`*p`).

Därför blir resultatet beroende av ordningen anropen görs i, och ordningen står bara i `newSnapshot`. `mergeSameNamedPlatser` måste också köras före `platsPlaces`. Det framgår inte av koden, bara av en kommentar. Den raderar ur `g.platser` medan den itererar. Det är tillåtet i Go men svårt att resonera om.

*Blandade ansvar.* `ortPlaces` gör fyra saker:
- räknar vilka kommuner orten ligger i
- väljer detaljtext
- avgör om orten ska utelämnas
- bygger resultatet

`finalize` fyller i standardvärden (`Query`), sorterar mätpunkter, räknar antal, bygger matchningsnycklar och sorterar platserna.

*Namngivning.*
- Variabler som `g`, `ks`, `o`, `k`, `p`, `st` och `m` är för korta för att förstå utan att läsa hela funktionen. `ks` är en mängd kommunnamn, men det syns inte.
- `add` i `groupStations` är en closure som tar fyra argument, bland annat en `create`-funktion. Den gör både "hämta eller skapa" och "lägg till mätpunkt".
- `subCount` håller antal platser per delnamn, men namnet säger inte det.
- `names` i `place` kallas "vikta namn". I koden heter operationen `fold`, så ordet "vikta" betyder normaliserade.

**Förslag.** Låt `ortPlaces` och `platsPlaces` arbeta på kopior, eller byt namn till `buildOrtPlaces` så att det framgår att de bygger något. Bryt ut `kommunerOf(p)` ur `ortPlaces`. Döp om `ks` till `kommunSet` och `add` till `addStation`. Flytta sorteringen av mätpunkter ur `finalize`.

---

## 5. Rangordningen kräver kommentarer för att kunna läsas

**Kategori:** kod som är svår att läsa utan kommentarer.
**Plats:** [matcher.go:37-71](../internal/places/matcher.go#L37-L71), [rank.go:53-75](../internal/places/rank.go#L53-L75)

**Problem.**

*Magiska tal.* `matchScore.weight()` räknar med talvärdena hos `matchClass`:
- `int(classContains) - 1 + int(s.typos)`
- `int(s.class) - 1`

Det fungerar bara så länge ordningen på konstanterna är exakt `Exact, Prefix, Word, Contains, Typo`. Lägger någon till en klass i mitten flyttas alla vikter utan att något test behöver gå sönder. Kommentaren ovanför förklarar utfallet (`-1`, `0`, `1`, `2`, `2+n`) men koden kräver att man läser den för att kunna ändra något.

*Dold koppling.* `viaOrtPenalty = 3` är vald för att träffen ska hamna efter orter och kommuner och dela rang med ett skrivfel. Kommentaren säger att det är avsiktligt, men värdet hänger på `weight()`-skalan. Ändras skalan stämmer inte `3` längre.

*Felaktig hänvisning.* Kommentaren på [matcher.go:41](../internal/places/matcher.go#L41) säger att `kindOrder` avgör. Funktionen heter `Kind.order()` och anropas från `rank`. Namnet `kindOrder` finns inte i koden.

*Blandade ansvar i `resolve`.* [rank.go:53-75](../internal/places/rank.go#L53-L75) gör tre saker:
- slår upp exakta namn
- rankar
- avgör tvetydighet

Den sista regeln ("två träffar med samma etikett och samma poäng") är ett affärsbeslut som bara kommentaren förklarar. Namnen `hits[0]` och `hits[1]` säger inte vad de är: bäst och tvåa.

*Namngivning.* `m` är matcher, `p` är `*place` (privat) men `Place` är den publika typen med nästan samma namn. `place` och `Place` i samma paket är lätta att förväxla. `qs` och `qWord` är krypterade; `qLower`/`qPrefixedBySpace` hade förklarat sig själva.

**Förslag.** Ge `matchClass` en egen `baseWeight()`-metod med en `switch` så att vikterna blir explicita. Härled `viaOrtPenalty` ur skalan, eller testa att en träff via orten alltid rankas efter kommuner och orter. Byt `kindOrder` i kommentaren mot `Kind.order`. Bryt ut tvetydighetsregeln till `isAmbiguous(hits)`. Döp om den privata typen `place` till `entry` eller `indexedPlace`.

---

## Kontrollerat utan anmärkning

- `internal/feed/feed.go`: tydliga ansvar, panikskydd, felhantering och femminutersregel. Namnen är bra.
- `internal/tnu/client.go`: felmeddelanden läcker inte `cli` eller `sign` (rad 92–96).
- `internal/config/config.go`: kort, lätt att läsa, tydliga felmeddelanden.
- `internal/server/handlers.go`: små funktioner med ett ansvar. `snapshot()` skiljer rätt mellan olika fel.
- `healthcheck.go`: ofarlig. `io.Copy` och `w.Write` i `handleHealth` ignorerar fel, vilket är rimligt här.

## Mindre fynd

- `tnu.AllStations`: kommentaren nämner parametern "p" som inte finns ([client.go:71](../internal/tnu/client.go#L71)). Det är en kvarglömd rest.
- `tnu.get(ctx, params string, ...)`: `params` är en färdig frågesträng, och anropet `c.get(ctx, "verbose", &resp)` visar inte vad "verbose" är. Funktionen gör också fyra saker: budgetkontroll, anrop, statushantering och avkodning.
- `main.run` startar index, server och nedstängning i en funktion på 50 rader. Det är läsbart men nära gränsen.
