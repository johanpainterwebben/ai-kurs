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

Första gången, innan den automatiska driftsättningen i steg 11 är uppsatt, byggs imagen på servern:

```bash
cd <appkatalog>
sudo docker compose -f docker-compose.yml -f compose.build.yml up -d --build
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

När steg 11 är uppsatt sköts nya versioner automatiskt: en push till `main` som klarar CI driftsätts inom ett par minuter, och en version som inte blir frisk rullas tillbaka av sig själv.

Säkerhetsuppdateringar i Go och basimagen kommer som pull requests från Dependabot. När de mergas byggs och driftsätts en ny image. Den omvända proxyn och tunnelklienten uppdateras för hand ungefär en gång i månaden:

```bash
cd <serverrepot> && sudo docker compose pull && sudo docker compose up -d
```

Rulla tillbaka för hand till en tidigare version (digesten för varje version står i sammanfattningen av CI-körningen på GitHub, och den förra versionen i `/var/lib/temperatur-deploy/previous`):

```bash
cd <appkatalog>
sudo docker pull ghcr.io/<github-konto>/<repo>@sha256:<digest>
sudo IMAGE_REF=ghcr.io/<github-konto>/<repo>@sha256:<digest> docker compose up -d
echo sha256:<digest> | sudo tee /var/lib/temperatur-deploy/current
```

Den automatiska driftsättningen fortsätter sedan med nästa nya version. Vill du stoppa den under tiden: `sudo systemctl stop pull-deploy.timer`.

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

## 11. Automatisk driftsättning (CI/CD)

### Så fungerar det

```
push/merge till main
  └─► GitHub Actions (.github/workflows/ci.yml)
        ├─ Go:     gofmt, go vet, go test -race, benchmark, govulncheck
        ├─ Lint:   golangci-lint
        ├─ Docker: bygge och sårbarhetsskanning (Trivy)
        └─ Publicera och signera (bara på main, bara om allt ovan lyckas):
             bygg → Trivy → publicera på GHCR (<commit-SHA> och main) → signera med cosign
                                     │
                                     ▼
servern: pull-deploy.timer (varje minut)
  └─ pull-deploy.sh
       1. vilken digest pekar taggen main på? samma som nu → klart
       2. kontrollera signaturen (rätt repo, workflow och gren) → annars stopp och larm
       3. hämta och starta den nya versionen
       4. vänta på att containern blir "healthy" (Dockers HEALTHCHECK mot /healthz)
       5. inte frisk inom HEALTH_TIMEOUT → starta föregående version igen och larma
```

Servern har inga öppna portar, så GitHub kan inte skicka något dit. Servern frågar i stället registret själv, och kör bara imagar som CI har byggt, testat, skannat och signerat. Ingen kod från GitHub körs på servern.

Varje start av appen gör ett anrop mot temperatur.nu. En ny version startas därför högst en gång, och en version som har rullats tillbaka försöks inte igen. En version ger högst två extra anrop.

### GitHub Secrets

**Inga egna hemligheter behövs i GitHub.** Pipelinen använder bara det inbyggda `GITHUB_TOKEN`, som GitHub skapar för varje körning:

- `packages: write` för att publicera imagen på GHCR, och bara i publiceringsjobbet.
- `id-token: write` för att signera med cosign utan nyckel. GitHub ger ett kortlivat OIDC-certifikat som visar vilket repo, workflow och gren som byggde imagen, så det finns ingen signeringsnyckel att förvara eller läcka.

Den enda hemligheten för driftsättningen ligger **på servern**, inte i GitHub: en token som bara får läsa paket (se nedan). Appens hemligheter (`TNU_CLI`, `TNU_CLI_KEY`) ligger som förut i `.env` på servern.

### Ställ in GitHub

1. **Skydda `main`** (Settings → Rules → Rulesets, eller Branches): kräv pull request, kräv att kontrollerna **Go**, **Lint** och **Docker** är gröna, och förbjud force-push och borttagning.
2. **Environment `production`** skapas automatiskt vid första körningen (Settings → Environments). Där kan du begränsa den till grenen `main` och, om du vill, kräva ett godkännande innan publiceringen körs.
3. **Paketets synlighet:** efter första publiceringen finns imagen under Packages. Är den privat behöver servern en token (steg 2 nedan). Är den publik behövs ingen. Imagen innehåller inga hemligheter, men koden går att läsa ut ur den.

### Ställ in servern

1. **Installera cosign** enligt https://docs.sigstore.dev/cosign/system_config/installation/ (ladda ned binären för `linux-amd64` från GitHub-releasen och kontrollera checksumman). `docker buildx` följer med Dockers officiella paket.

2. **Logga in mot GHCR** (bara om paketet är privat). Skapa en *classic* personal access token på GitHub med **endast** behörigheten `read:packages`, och logga in som root (skriptet körs som root):

   ```bash
   sudo docker login ghcr.io -u <github-konto> --password-stdin   # klistra in token, avsluta med Ctrl+D
   ```

   Inloggningen sparas i `/root/.docker/config.json`. Läcker token kan den bara läsa paket. Ta bort den på GitHub och logga in med en ny.

3. **Installera skriptet och inställningarna:**

   ```bash
   cd <appkatalog>
   sudo install -m 0755 deploy/pull-deploy.sh /usr/local/bin/pull-deploy
   sudo install -m 0600 deploy/deploy.env.example /etc/temperatur-deploy.env
   sudo nano /etc/temperatur-deploy.env      # IMAGE, APP_DIR, SIGNER_IDENTITY, ev. NOTIFY_URL
   ```

4. **Prova en gång för hand** när CI har publicerat minst en version:

   ```bash
   sudo /usr/local/bin/pull-deploy /etc/temperatur-deploy.env
   sudo docker compose -f <appkatalog>/docker-compose.yml ps    # STATUS ska säga "healthy"
   ```

5. **Slå på timern:**

   ```bash
   sudo install -m 0644 deploy/pull-deploy.service deploy/pull-deploy.timer /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now pull-deploy.timer
   systemctl list-timers pull-deploy.timer     # nästa körning
   journalctl -u pull-deploy -f                # vad som händer
   ```

Efter en ändring av skriptet eller enheterna i repot installeras de igen med samma kommandon.

### Felsökning

| Symptom | Trolig orsak | Åtgärd |
|---|---|---|
| `unauthorized` eller `denied` från registret | Servern är inte inloggad, eller token saknar `read:packages` | Steg 2 ovan |
| `signaturen stämmer inte` | `SIGNER_IDENTITY` stämmer inte med repot/workflowet, eller imagen är inte signerad | Kontrollera värdet i `/etc/temperatur-deploy.env` mot repots namn |
| `blev inte frisk … tillbaka på` | Den nya versionen startar inte eller svarar inte på `/healthz` | `sudo docker compose logs` i `<appkatalog>`; rätta och pusha en ny version |
| Inget händer efter en push | CI misslyckades, eller timern är avslagen | Actions-fliken på GitHub; `systemctl status pull-deploy.timer` |
| En version försöks inte igen | Den finns i `/var/lib/temperatur-deploy/rejected` | Avsiktligt; pusha en rättad version, eller ta bort filen för att försöka igen |

## Innan adressen sprids

- [ ] `TNU_CLI_KEY` från temperatur.nu ligger i `.env` (loggen säger `signerar anrop mot temperatur.nu`).
- [ ] Källhänvisningen till temperatur.nu syns längst ned på sidan (finns redan).
- [ ] Sajten är fortfarande icke-kommersiell – annars krävs ett avtal med temperatur.nu.
- [ ] Övervakning är uppsatt (steg 9).
