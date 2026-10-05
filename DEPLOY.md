# Driftsättning på en Debian-server

Plan för att ta "Vad ska jag ha för kläder?" från repot till en publik sajt med HTTPS på en server med **Debian 13 (*trixie*)**.

> Installationsstegen (paket, paketkällor för Docker och Caddy, SSH-inställningen och Caddy-konfigurationen) är provkörda i en Debian 13-container. Det som kräver en riktig server – tjänster som startar vid uppstart, brandväggen och certifikatet – är inte provkört.

```
Besökare ──HTTPS──► Caddy (port 443, certifikat från Let's Encrypt)
                      │
                      ▼  127.0.0.1:8080 (bara lokalt)
                    Docker-container med appen ──HTTPS──► api.temperatur.nu
```

Caddy tar emot all trafik utifrån och sköter HTTPS. Appen lyssnar bara lokalt och syns aldrig direkt på internet.

Byt ut dessa platshållare överallt nedan:

| Platshållare | Betyder |
|---|---|
| `vadskajagha.example.se` | din domän |
| `deploy` | användaren du loggar in med på servern |
| `SERVERNS-IP` | serverns publika IP-adress |

---

## 0. Innan du börjar

- [ ] En Debian 13-server med publik IP och SSH-åtkomst. Appen är liten: 1 vCPU, 1 GB RAM och 5 GB disk räcker gott (bygget är det som kräver mest).
- [ ] Ett domännamn med en **A-post** (och AAAA-post om servern har IPv6) som pekar på `SERVERNS-IP`. Kontrollera med `dig +short vadskajagha.example.se`.
- [ ] Port **80 och 443** öppna i eventuell brandvägg hos hostingleverantören (krävs för certifikatet).
- [ ] `TNU_CLI`: din e-postadress.
- [ ] Helst `TNU_CLI_KEY`: en klientnyckel från temperatur.nu (kontakt via https://www.temperatur.nu/info/api/). Enligt villkoren ska appar med flera användare signera sina anrop. Sajten fungerar tekniskt utan nyckel (den gör bara ~6 anrop i timmen), men skaffa en innan sajten görs publik.

## 1. Grundsäkra servern

Logga in som root (eller med den användare leverantören gav dig). En minimal Debian 13 saknar bland annat `sudo`, `git` och `curl`, så de installeras först:

```bash
apt update && apt full-upgrade -y
apt install -y sudo git curl ca-certificates gpg nano
adduser deploy
usermod -aG sudo deploy
```

Lägg in din publika SSH-nyckel för `deploy` – kör detta **på din egen dator**:

```bash
ssh-copy-id deploy@SERVERNS-IP
```

Stäng av lösenordsinloggning och root-inloggning över SSH (på servern):

```bash
sudo tee /etc/ssh/sshd_config.d/99-hardening.conf >/dev/null <<'EOF'
PasswordAuthentication no
PermitRootLogin no
EOF
sudo systemctl reload ssh
```

> **Viktigt:** Testa att du kan logga in som `deploy` med nyckel i ett **nytt** terminalfönster innan du stänger det du har öppet.

Brandvägg – bara SSH, HTTP och HTTPS:

```bash
sudo apt install -y ufw
sudo ufw allow OpenSSH
sudo ufw allow 80,443/tcp
sudo ufw enable
```

> Docker går förbi ufw för portar som publiceras utåt. Därför binder `docker-compose.yml` appen till `127.0.0.1:8080` – den nås bara via Caddy. Ändra inte det till `8080:8080`.

Automatiska säkerhetsuppdateringar och korrekt klocka (krävs för HTTPS):

```bash
sudo apt install -y unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades   # svara "Ja"
timedatectl                                        # "System clock synchronized: yes"
```

## 2. Installera Docker

Från Dockers officiella paketförråd. Debians egna paket (`docker.io`) är ofta äldre, och `docker compose` (version 2 eller senare) behövs för `docker-compose.yml`. Paketkällan skrivs i det format (deb822, `.sources`) som Debian 13 använder:

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

Begränsa containerloggarnas storlek (annars växer de tills disken är full):

```bash
sudo tee /etc/docker/daemon.json >/dev/null <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
EOF
sudo systemctl restart docker
```

> Lägg **inte** till `deploy` i gruppen `docker` – det ger i praktiken root-behörighet. Kör Docker med `sudo`.

## 3. Hämta koden

Repot är privat. Skapa en **deploy key** som bara kan läsa just det här repot (`git` installerades i steg 1):

```bash
ssh-keygen -t ed25519 -f ~/.ssh/ai-kurs-deploy -N "" -C "deploy@vadskajagha"
cat ~/.ssh/ai-kurs-deploy.pub
```

Lägg in den publika nyckeln på GitHub: repot → *Settings* → *Deploy keys* → *Add deploy key*. Kryssa **inte** i "Allow write access".

```bash
cat >> ~/.ssh/config <<'EOF'
Host github-ai-kurs
  HostName github.com
  User git
  IdentityFile ~/.ssh/ai-kurs-deploy
  IdentitiesOnly yes
EOF

sudo mkdir -p /opt/vad-ska-jag-ha
sudo chown deploy:deploy /opt/vad-ska-jag-ha
git clone git@github-ai-kurs:johanpainterwebben/ai-kurs.git /opt/vad-ska-jag-ha
cd /opt/vad-ska-jag-ha
```

## 4. Konfigurera

```bash
cp .env.example .env
nano .env          # TNU_CLI=din@epost.se, TNU_CLI_KEY=nyckeln (om du har den)
chmod 600 .env
```

`.env` innehåller nyckeln – den ska aldrig checkas in (den finns redan i `.gitignore`).

## 5. Bygg och starta appen

```bash
sudo docker compose up -d --build
sudo docker compose ps                       # STATUS ska vara "Up"
sudo docker compose logs --tail 20
curl -s http://127.0.0.1:8080/healthz        # ska svara "ok"
curl -s "http://127.0.0.1:8080/api/search?q=Lund" | head -c 200; echo
```

I loggen ska det stå `hämtade alla mätpunkter antal=…` inom några sekunder. Saknas nyckeln står det också `osignerade anrop` – det är väntat tills du lagt in `TNU_CLI_KEY`.

Containern startar om av sig själv vid krasch och när servern startas om (`restart: unless-stopped`, och Docker startar vid uppstart).

## 6. HTTPS med Caddy

Installera Caddy från dess officiella paketförråd. (Caddys egen anvisning nämner även paketet `apt-transport-https` – det finns inte i Debian 13 och behövs inte, apt klarar HTTPS själv.)

```bash
sudo apt install -y debian-keyring debian-archive-keyring
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy
```

Ersätt `/etc/caddy/Caddyfile` med (konfigurationen är validerad med `caddy validate`):

```caddyfile
vadskajagha.example.se {
	encode zstd gzip

	header {
		Strict-Transport-Security "max-age=31536000"
		-Server
	}

	reverse_proxy 127.0.0.1:8080

	log {
		output file /var/log/caddy/vadskajagha.log {
			roll_size 10MiB
			roll_keep 5
			roll_keep_for 168h
		}
	}
}
```

```bash
sudo caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
sudo systemctl reload caddy
sudo journalctl -u caddy --since "5 min ago" | grep -i certificate
```

Caddy hämtar och förnyar certifikatet automatiskt. Det kräver att DNS pekar rätt och att port 80 och 443 är öppna.

Vad Caddy står för:

- **HTTPS och HSTS** – webbläsaren använder bara HTTPS. Appens övriga säkerhetsrubriker (CSP m.m.) skickas vidare oförändrade.
- **Komprimering** (`encode`) – HTML, CSS, JS och JSON krymper till ungefär en fjärdedel till en tredjedel av storleken.
- **Åtkomstloggar** – innehåller besökarnas IP-adresser, som är personuppgifter. De sparas därför bara i 7 dagar.

> Anropsbegränsning per IP-adress finns inte i Caddys standardversion (kräver pluginet `caddy-ratelimit` och en egenbyggd Caddy). Appen är byggd för att tåla många anrop – en sökning kostar under en millisekund och inga anrop mot temperatur.nu – så det kan vänta tills det behövs.

## 7. Kontrollera

- [ ] `https://vadskajagha.example.se` öppnas med giltigt certifikat, och en sökning fungerar.
- [ ] `http://vadskajagha.example.se` skickas vidare till HTTPS.
- [ ] `curl -sI https://vadskajagha.example.se | grep -iE 'strict-transport|content-security'` visar båda rubrikerna.
- [ ] Testa gärna på https://www.ssllabs.com/ssltest/ (mål: A eller bättre) och https://securityheaders.com/.
- [ ] Efter en halvtimme: `sudo docker compose logs | grep -c "hämtade alla mätpunkter"` ger ungefär 3–4 (en hämtning var 10:e minut). Fler än så tyder på fel – då riskerar API-villkoren att brytas.

## 8. Uppdatera och rulla tillbaka

Ny version:

```bash
cd /opt/vad-ska-jag-ha
git pull
sudo docker compose up -d --build
sudo docker image prune -f
```

En gång i månaden, för säkerhetsuppdateringar i Go och basimagen (även om koden inte ändrats):

```bash
sudo docker compose build --pull && sudo docker compose up -d
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

- **Övervakning:** Lägg upp en extern koll mot `https://vadskajagha.example.se/healthz` (t.ex. UptimeRobot eller Uptime Kuma) som larmar om sajten inte svarar.
- **Loggar:** `sudo docker compose logs -f`. Håll utkik efter `listan räknas nu som inaktuell` – då har hämtningen från temperatur.nu misslyckats fyra gånger i rad och sajten visar en varning för besökarna.
- **Säkerhetskopiering:** Det enda som inte går att återskapa från repot är `.env`. Spara den på ett säkert ställe. Caddys certifikat hämtas automatiskt på nytt.

## 10. Felsökning

| Symptom | Trolig orsak | Åtgärd |
|---|---|---|
| Containern avslutas direkt, loggen säger `TNU_CLI saknas` | `.env` saknas eller är tom | Steg 4 |
| Caddy får inget certifikat | DNS pekar inte rätt, eller port 80/443 stängd | `dig +short vadskajagha.example.se`, kontrollera brandväggar |
| `502 Bad Gateway` från Caddy | Appen kör inte | `sudo docker compose ps` och `logs` |
| Gul varning "Temperaturerna kan vara inaktuella" | Fyra misslyckade hämtningar i rad | Loggen visar orsaken; kontrollera nätverket och att klienten inte är blockerad hos temperatur.nu |
| Loggen säger `klienten är blockerad` | temperatur.nu har blockerat `cli` | Kontakta temperatur.nu; skaffa/lägg in `TNU_CLI_KEY` |

## Innan sajten görs publik

- [ ] `TNU_CLI_KEY` från temperatur.nu ligger i `.env` (loggen säger `signerar anrop mot temperatur.nu`).
- [ ] Källhänvisningen till temperatur.nu syns längst ned på sidan (finns redan).
- [ ] Sajten är fortfarande icke-kommersiell – annars krävs ett avtal med temperatur.nu.
- [ ] Övervakning är uppsatt (steg 9).
