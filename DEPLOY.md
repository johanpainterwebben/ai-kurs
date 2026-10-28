# Driftsättning

Så här körs "Vad ska jag ha för kläder?" på **https://temperatur.pilros.se**: på en egen dator med **Debian 13 (*trixie*)** bakom en vanlig router, i Docker, publicerad via **Cloudflare Tunnel**.

```
Besökare ──HTTPS──► Cloudflare ══tunnel══► cloudflared ──► nginx ──► temperatur:8080 ──HTTPS──► api.temperatur.nu
                    (*.pilros.se)          └──── ~/Projects/server ────┘   └── det här repot
```

- **Inga portar är öppna i routern.** `cloudflared` kopplar upp sig *utåt* mot Cloudflare, och besökarnas trafik kommer tillbaka genom den krypterade tunneln.
- **HTTPS och certifikat sköts av Cloudflare.**
- **Nginx och tunneln är gemensamma för alla tjänster på datorn** och ligger i `~/Projects/server`, inte i det här repot. Där finns också en README om hur nya tjänster läggs till. Appen ansluter till deras Docker-nätverk `proxy`, och nginx väljer tjänst utifrån värdnamnet.
- **`pilros.se` utan subdomän svarar inte alls** (finns inte i DNS). Okända subdomäner får en enkel 404-sida.

---

## 0. Innan du börjar

- [ ] Domänen (`pilros.se`) använder Cloudflares namnservrar.
- [ ] En tunnel är skapad i Cloudflare Zero Trust → Networks → Tunnels, med token sparad.
- [ ] `TNU_CLI`: din e-postadress.
- [ ] Helst `TNU_CLI_KEY`: en klientnyckel från temperatur.nu (kontakt via https://www.temperatur.nu/info/api/). Enligt villkoren ska appar med flera användare signera sina anrop. Sajten fungerar tekniskt utan nyckel (den gör bara ~6 anrop i timmen), men skaffa en innan adressen sprids.

## 1. Grundsäkra datorn

Kör som root (`su -`, med bindestreck så att `/usr/sbin` kommer med) tills ditt vanliga konto har `sudo`:

```bash
apt update && apt full-upgrade -y
apt install -y sudo git curl ca-certificates
usermod -aG sudo johan        # logga sedan ut från skrivbordet och in igen
```

Därefter används det vanliga kontot med `sudo`.

Automatiska säkerhetsuppdateringar (svara "Ja"):

```bash
sudo apt install -y unattended-upgrades apt-listchanges
sudo dpkg-reconfigure -plow unattended-upgrades
```

Brandvägg – inget inkommande, allt utgående (tunneln går utåt och påverkas inte):

```bash
sudo apt install -y ufw
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw enable
```

Stäng av tjänster som inte behövs, om datorn inte ska skriva ut:

```bash
sudo systemctl disable --now cups cups-browsed avahi-daemon
```

> **Lägg inte till kontot i gruppen `docker`** – det ger root utan lösenord. Kör Docker med `sudo`. Ta bort det om det redan är gjort: `sudo gpasswd -d johan docker`.

Slå på **tvåfaktorsinloggning** för Cloudflare- och GitHub-kontona. Den som kommer åt Cloudflare kan ta över domänen och tunneln.

## 2. Installera Docker

Från Dockers officiella paketförråd (Debians `docker.io` är ofta äldre, och `docker compose` v2 behövs):

```bash
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Signed-By: /etc/apt/keyrings/docker.asc
EOF
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo docker run --rm hello-world
```

Loggstorleken begränsas i varje `docker-compose.yml` (3 × 10 MB per container).

## 3. Hämta koden

Repot är privat. Enklast med GitHub CLI, som loggar in via webbläsaren:

```bash
sudo apt install -y gh
gh auth login                  # GitHub.com → HTTPS → Login with a web browser
mkdir -p ~/Projects && cd ~/Projects
gh repo clone johanpainterwebben/ai-kurs
```

## 4. Den gemensamma ingången (nginx + tunnel)

Ligger i `~/Projects/server` och startas **före** appen, eftersom den skapar nätverket `proxy`. Fullständig beskrivning finns i `~/Projects/server/README.md`; i korthet:

1. `tunnel.env` med `TUNNEL_TOKEN=…` (`chmod 600`).
2. I Cloudflare Zero Trust → Networks → Tunnels → din tunnel → **Published application routes** (hette tidigare *Public Hostname*): Subdomain `*`, Domain `pilros.se`, Service **HTTP** `nginx:8080`.
3. Under DNS för pilros.se: en CNAME-post `*` → `<tunnel-id>.cfargotunnel.com`, *Proxied* (orange moln). Ingen post för `pilros.se` själv.
4. Nginx-konfigurationen för appen finns i `nginx/conf.d/temperatur.pilros.se.conf`.

```bash
cd ~/Projects/server
sudo docker compose up -d
sudo docker compose exec nginx nginx -t
sudo docker compose logs cloudflared | grep -i "registered tunnel"
```

Rekommenderade inställningar i Cloudflare (gratis):

- SSL/TLS → Edge Certificates: **Always Use HTTPS** på, **Minimum TLS Version** 1.2.
- Security → Bots: **Bot Fight Mode** på.
- Security → WAF: Cloudflares **Managed rules** på.

## 5. Konfigurera appen

```bash
cd ~/Projects/ai-kurs
cp .env.example .env
nano .env          # TNU_CLI=din@epost.se, TNU_CLI_KEY=nyckeln (om du har den)
chmod 600 .env
```

`.env` innehåller nyckeln – den ska aldrig checkas in (den finns redan i `.gitignore`).

## 6. Bygg och starta appen

```bash
cd ~/Projects/ai-kurs
sudo docker compose up -d --build
sudo docker compose ps                       # STATUS ska vara "Up"
sudo docker compose logs --tail 20
curl -s http://127.0.0.1:8080/healthz        # svarar "ok" (se nedan)
curl -s "http://127.0.0.1:8080/api/search?q=Lund" | head -c 200; echo
```

`/healthz` svarar alltid 200 så länge processen lever, med texten `ok`, `ok: väntar på första hämtningen` (precis efter start) eller `ok: listan kan vara inaktuell` (flera hämtningar i rad har misslyckats). Docker kör samma kontroll själv (`HEALTHCHECK`, se `docker compose ps` – STATUS ska säga `healthy`).

I loggen ska det stå `hämtade alla mätpunkter antal=…` inom några sekunder. Saknas nyckeln står det också `osignerade anrop` – det är väntat tills du lagt in `TNU_CLI_KEY`.

Containrarna startar om av sig själva vid krasch och när datorn startas om (`restart: unless-stopped`, och Docker startar vid uppstart).

## 7. Kontrollera

- [ ] `https://temperatur.pilros.se` öppnas och en sökning fungerar.
- [ ] `curl -sI https://temperatur.pilros.se | grep -iE 'strict-transport|content-security'` visar båda rubrikerna.
- [ ] `https://pilros.se` ger inget svar (namnet finns inte), och `https://finnsinte.pilros.se` ger 404-sidan utan nämnande av nginx.
- [ ] Testa gärna på https://www.ssllabs.com/ssltest/ och https://securityheaders.com/.
- [ ] Efter en halvtimme: `sudo docker compose logs | grep -c "hämtade alla mätpunkter"` ger ungefär 3–4 (en hämtning var 10:e minut). Fler än så tyder på fel – då riskerar API-villkoren att brytas.

> Har du nyligen besökt en subdomän innan den fanns i DNS kan routern minnas "finns inte" i upp till 30 minuter. Testa från mobilen utan wifi, vänta, eller starta om routern.

## 8. Uppdatera och rulla tillbaka

Ny version:

```bash
cd ~/Projects/ai-kurs
git pull
sudo docker compose up -d --build
sudo docker image prune -f
```

En gång i månaden, för säkerhetsuppdateringar i Go, basimagen, nginx och cloudflared (även om koden inte ändrats):

```bash
cd ~/Projects/ai-kurs && sudo docker compose build --pull && sudo docker compose up -d
cd ~/Projects/server && sudo docker compose pull && sudo docker compose up -d
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

- **Övervakning:** Lägg upp en extern koll mot `https://temperatur.pilros.se/healthz` (t.ex. UptimeRobot) som larmar om sajten inte svarar.
- **Loggar:** `sudo docker compose logs -f` här, och `nginx`/`cloudflared` i `~/Projects/server`. Håll utkik efter `datan räknas nu som inaktuell` – då har hämtningen från temperatur.nu misslyckats fyra gånger i rad och sajten visar en varning för besökarna.
- **Säkerhetskopiering:** Det som inte går att återskapa från repona är `.env` och `~/Projects/server/tunnel.env`. Spara dem på ett säkert ställe. Läcker tunnelns token: skapa en ny i Cloudflare, lägg in den i `tunnel.env` och starta om `cloudflared`.

## 10. Felsökning

| Symptom | Trolig orsak | Åtgärd |
|---|---|---|
| Containern avslutas direkt, loggen säger `TNU_CLI saknas` | `.env` saknas eller är tom | Steg 5 |
| `network proxy declared as external, but could not be found` | Den gemensamma ingången är inte startad | Steg 4 först |
| Webbläsaren hittar inte adressen | DNS-posten `*` saknas, eller gammalt "finns inte" i routerns minne | Steg 4.3; se notisen i steg 7 |
| Cloudflares felsida 1033 | `cloudflared` kör inte eller har fel token | `sudo docker compose logs cloudflared` i `~/Projects/server` |
| "Tjänsten är inte tillgänglig just nu" (502) | Appen kör inte, eller är inte ansluten till `proxy` | `sudo docker compose ps` och `logs` här |
| 404-sidan på temperatur.pilros.se | Nginx saknar filen för värdnamnet | `nginx/conf.d/temperatur.pilros.se.conf` i `~/Projects/server` |
| `service "nginx" is not running` | Kommandot kördes i fel mapp | Nginx-kommandon körs i `~/Projects/server` |
| `usermod: command not found` som root | Root via `su` utan bindestreck | `su -`, eller `/usr/sbin/usermod` |
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
