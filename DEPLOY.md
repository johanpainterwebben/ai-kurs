# Driftsättning

Så här driftsätts "Vad ska jag ha för kläder?" i allmänhet: på en egen Linux-server i Docker, bakom en omvänd proxy och en utgående tunnel, så att servern inte behöver några öppna inkommande portar. Exakta värden för en viss installation (domän, konton, sökvägar och vilka verktyg som används) hör hemma i driftmiljöns eget repo och checkas inte in här.

```
Besökare ──HTTPS──► tunnel-/CDN-leverantör ══tunnel══► tunnelklient ──► omvänd proxy ──► temperatur:8080 ──HTTPS──► api.temperatur.nu
                    (*.<din-domän>)                    └──────── serverrepot ────────┘   └── det här repot
```

- **Inga inkommande portar är öppna.** Tunnelklienten kopplar upp sig *utåt* mot leverantören, och besökarnas trafik kommer tillbaka genom den krypterade tunneln.
- **HTTPS och certifikat sköts av tunnel-/CDN-leverantören.**
- **Den omvända proxyn och tunneln är gemensamma för alla tjänster på servern** och ligger i ett separat serverrepo, inte i det här repot. Appen ansluter till deras delade Docker-nätverk (namnet står i `docker-compose.yml`), och proxyn väljer tjänst utifrån värdnamnet.
- Domänen utan subdomän behöver inte svara alls. Okända subdomäner bör få en enkel 404-sida.

Platshållare i den här filen:

| Platshållare | Betyder |
|---|---|
| `<din-domän>` | Domänen som tjänsterna publiceras under |
| `<värdnamn>` | Appens fullständiga värdnamn, t.ex. en subdomän till `<din-domän>` |
| `<användare>` | Ditt vanliga konto på servern |
| `<github-konto>/<repo>` | Var det här repot ligger |
| `<serverrepot>` | Katalogen med den omvända proxyn och tunneln |
| `<appkatalog>` | Katalogen där det här repot är klonat |

---

## 0. Innan du börjar

- [ ] Domänen hanteras av tunnel-/CDN-leverantören.
- [ ] En tunnel är skapad hos leverantören, och dess token är sparad.
- [ ] `TNU_CLI`: din e-postadress.
- [ ] Helst `TNU_CLI_KEY`: en klientnyckel från temperatur.nu (kontakt via https://www.temperatur.nu/info/api/). Enligt villkoren ska appar med flera användare signera sina anrop. Sajten fungerar tekniskt utan nyckel (den gör bara ~6 anrop i timmen), men skaffa en innan adressen sprids.

## 1. Grundsäkra servern

- Installera säkerhetsuppdateringar och slå på automatiska säkerhetsuppdateringar.
- Använd ett vanligt konto (`<användare>`) med `sudo`, inte root.
- Brandvägg: neka allt inkommande och tillåt utgående. Tunneln går utåt och påverkas inte.
- Stäng av tjänster som inte behövs (t.ex. utskrift och tjänstupptäckt på lokala nätverket).
- Slå på **tvåfaktorsinloggning** för kontona hos tunnel-/CDN-leverantören och GitHub. Den som kommer åt leverantörskontot kan ta över domänen och tunneln.

> **Lägg inte till kontot i gruppen `docker`** – det ger root utan lösenord. Kör Docker med `sudo`. Ta bort det om det redan är gjort: `sudo gpasswd -d <användare> docker`.

## 2. Installera Docker

Installera Docker Engine och `docker compose` v2 från Dockers officiella paketförråd för din distribution (distributionernas egna paket är ofta äldre). Kontrollera installationen:

```bash
sudo docker run --rm hello-world
```

Loggstorleken begränsas i varje `docker-compose.yml` (3 × 10 MB per container).

## 3. Hämta koden

Repot är privat. Enklast med GitHub CLI, som loggar in via webbläsaren:

```bash
gh auth login                  # GitHub.com → HTTPS → Login with a web browser
gh repo clone <github-konto>/<repo> <appkatalog>
```

## 4. Den gemensamma ingången (omvänd proxy + tunnel)

Ligger i `<serverrepot>` och startas **före** appen, eftersom den skapar det delade Docker-nätverket. Hur den sätts upp beskrivs i serverrepots egen dokumentation. Kraven från appens sida:

1. Tunnelns token ligger i en egen fil med rättigheterna `600` och checkas aldrig in.
2. Tunneln skickar trafik för `*.<din-domän>` till den omvända proxyn. DNS-posten för `*` pekar på tunneln och går via leverantörens proxy.
3. Proxyn skickar `<värdnamn>` vidare till `temperatur:8080` på det delade nätverket och svarar med 404 för okända värdnamn utan att avslöja vilken programvara den kör.
4. `/healthz` bör bara nås internt (se `docs/sakerhetsgranskning-2026-10-28.md`, F4), och proxyn bör begränsa antalet anrop per klient mot `/api/*` (F2).

Rekommenderade inställningar hos tunnel-/CDN-leverantören:

- Alltid HTTPS och lägst TLS 1.2.
- Skydd mot botar.
- Leverantörens hanterade brandväggsregler (WAF).

## 5. Konfigurera appen

```bash
cd <appkatalog>
cp .env.example .env
nano .env          # TNU_CLI=din@epost.se, TNU_CLI_KEY=nyckeln (om du har den)
chmod 600 .env
```

`.env` innehåller nyckeln – den ska aldrig checkas in (den finns redan i `.gitignore`).

## 6. Bygg och starta appen

```bash
cd <appkatalog>
sudo docker compose up -d --build
sudo docker compose ps                       # STATUS ska vara "Up"
sudo docker compose logs --tail 20
curl -s http://127.0.0.1:8080/healthz        # svarar "ok" (se nedan)
curl -s "http://127.0.0.1:8080/api/search?q=Lund" | head -c 200; echo
```

`/healthz` svarar alltid 200 så länge processen lever, med texten `ok`, `ok: väntar på första hämtningen` (precis efter start) eller `ok: listan kan vara inaktuell` (flera hämtningar i rad har misslyckats). Docker kör samma kontroll själv (`HEALTHCHECK`, se `docker compose ps` – STATUS ska säga `healthy`).

I loggen ska det stå `hämtade alla mätpunkter antal=…` inom några sekunder. Saknas nyckeln står det också `osignerade anrop` – det är väntat tills du lagt in `TNU_CLI_KEY`.

Containrarna startar om av sig själva vid krasch och när servern startas om (`restart: unless-stopped`, och Docker startar vid uppstart).

## 7. Kontrollera

- [ ] `https://<värdnamn>` öppnas och en sökning fungerar.
- [ ] `curl -sI https://<värdnamn> | grep -iE 'strict-transport|content-security'` visar båda rubrikerna.
- [ ] `https://<din-domän>` ger inget svar, och en okänd subdomän ger 404-sidan utan att avslöja vilken programvara proxyn kör.
- [ ] Testa gärna på https://www.ssllabs.com/ssltest/ och https://securityheaders.com/.
- [ ] Efter en halvtimme: `sudo docker compose logs | grep -c "hämtade alla mätpunkter"` ger ungefär 3–4 (en hämtning var 10:e minut). Fler än så tyder på fel – då riskerar API-villkoren att brytas.

> Har du nyligen besökt en subdomän innan den fanns i DNS kan den lokala DNS-cachen minnas "finns inte" en stund. Testa från ett annat nät eller vänta.

## 8. Uppdatera och rulla tillbaka

Ny version:

```bash
cd <appkatalog>
git pull
sudo docker compose up -d --build
sudo docker image prune -f
```

En gång i månaden, för säkerhetsuppdateringar i Go, basimagen, den omvända proxyn och tunnelklienten (även om koden inte ändrats):

```bash
cd <appkatalog> && sudo docker compose build --pull && sudo docker compose up -d
cd <serverrepot> && sudo docker compose pull && sudo docker compose up -d
```

Rulla tillbaka till en tidigare version:

```bash
git log --oneline | head          # hitta commiten som fungerade
git checkout <commit>
sudo docker compose up -d --build
git checkout main                 # när felet är rättat och du vill tillbaka
```

> Varje omstart av appen gör ett nytt anrop mot temperatur.nu. Starta inte om den i en loop – vänta minst fem minuter mellan omstarter om du felsöker.

## 9. Löpande drift

- **Övervakning:** Lägg upp en extern tjänst som kontrollerar `https://<värdnamn>/healthz` och larmar om sajten inte svarar.
- **Loggar:** `sudo docker compose logs -f` här, och proxyns och tunnelns loggar i `<serverrepot>`. Håll utkik efter `datan räknas nu som inaktuell` – då har hämtningen från temperatur.nu misslyckats fyra gånger i rad och sajten visar en varning för besökarna.
- **Säkerhetskopiering:** Det som inte går att återskapa från repona är `.env` och tunnelns token. Spara dem på ett säkert ställe. Läcker tunnelns token: skapa en ny hos leverantören, lägg in den och starta om tunnelklienten.

## 10. Felsökning

| Symptom | Trolig orsak | Åtgärd |
|---|---|---|
| Containern avslutas direkt, loggen säger `TNU_CLI saknas` | `.env` saknas eller är tom | Steg 5 |
| `network … declared as external, but could not be found` | Den gemensamma ingången är inte startad | Steg 4 först |
| Webbläsaren hittar inte adressen | DNS-posten saknas, eller ett gammalt "finns inte" i DNS-cachen | Steg 4; se notisen i steg 7 |
| Leverantörens felsida om att tunneln inte svarar | Tunnelklienten kör inte eller har fel token | Tunnelklientens logg i `<serverrepot>` |
| "Tjänsten är inte tillgänglig just nu" (502) | Appen kör inte, eller är inte ansluten till det delade nätverket | `sudo docker compose ps` och `logs` här |
| 404-sidan på `<värdnamn>` | Proxyn saknar konfiguration för värdnamnet | Proxyns konfiguration i `<serverrepot>` |
| Gul varning "Temperaturerna kan vara inaktuella" | Fyra misslyckade hämtningar i rad | Loggen visar orsaken; kontrollera nätverket och att klienten inte är blockerad hos temperatur.nu |
| Loggen säger `klienten är blockerad` | temperatur.nu har blockerat `cli` | Kontakta temperatur.nu; skaffa/lägg in `TNU_CLI_KEY` |

## CI/CD (GitHub Actions)

- **`.github/workflows/ci.yml`** körs för varje PR och push till `main`: formatering, `go vet`, `go test -race` (inklusive kontrasttestet), benchmarken för `Suggest`, `govulncheck`, `golangci-lint` samt Docker-bygge med Trivy-skanning.
- **`.github/workflows/publish.yml`** körs efter merge till `main`: bygger imagen, skannar den och publicerar den på GHCR som `ghcr.io/<ägare>/<repo>:<commit-SHA>`.
- Actions är låsta till commit-SHA (Dependabot föreslår uppdateringar) och workflows har minsta möjliga `permissions`. Inga hemligheter används av CI; `publish` använder bara det inbyggda `GITHUB_TOKEN`.
- **Driftsättningen på servern är inte automatiserad än.** Servern har inga öppna portar, så den måste hämta nya imagar själv: antingen en self-hosted runner som bara kör jobb från `main`, eller en tjänst på servern som hämtar nya taggar. Efter driftsättningen ska `/healthz` kontrolleras och föregående image återställas om den inte svarar. Tills dess gäller steg 8 ovan.

## Innan adressen sprids

- [ ] `TNU_CLI_KEY` från temperatur.nu ligger i `.env` (loggen säger `signerar anrop mot temperatur.nu`).
- [ ] Källhänvisningen till temperatur.nu syns längst ned på sidan (finns redan).
- [ ] Sajten är fortfarande icke-kommersiell – annars krävs ett avtal med temperatur.nu.
- [ ] Övervakning är uppsatt (steg 9).
