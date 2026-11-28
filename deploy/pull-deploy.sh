#!/usr/bin/env bash
# Hämtar och startar en ny version av appen när CI har publicerat en.
#
# Körs av systemd-timern pull-deploy.timer (se DEPLOY.md, "Automatisk
# driftsättning"). Servern har inga öppna portar, så GitHub kan inte skicka
# nya versioner hit. I stället frågar servern registret vilken image taggen
# pekar på, och startar den bara om den är signerad av repots CI-jobb på main.
#
# Varje start av appen gör ett anrop mot temperatur.nu. En ny version startas
# därför högst en gång, och en version som har rullats tillbaka försöks inte
# igen. En version ger alltså högst två extra anrop (start och återställning).
#
# Användning: pull-deploy [inställningsfil]   (standard /etc/temperatur-deploy.env)
set -euo pipefail

config=${1:-/etc/temperatur-deploy.env}
# shellcheck source=deploy.env.example
source "$config"

: "${IMAGE:?IMAGE saknas i $config}"
: "${APP_DIR:?APP_DIR saknas i $config}"
: "${SIGNER_IDENTITY:?SIGNER_IDENTITY saknas i $config}"
TAG=${TAG:-main}
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-120}
STATE_DIR=${STATE_DIR:-/var/lib/temperatur-deploy}

readonly OIDC_ISSUER=https://token.actions.githubusercontent.com
readonly SERVICE=temperatur
readonly HEALTH_POLL_SECONDS=5

log() { echo "pull-deploy: $*" >&2; }

# notify loggar och skickar, om NOTIFY_URL är satt, meddelandet dit (t.ex.
# en ntfy-kanal). Ett misslyckat larm får inte stoppa driftsättningen.
notify() {
  log "$1"
  [[ -n "${NOTIFY_URL:-}" ]] || return 0
  curl -fsS -m 10 -d "temperatur: $1" "$NOTIFY_URL" >/dev/null || log "kunde inte skicka larmet"
}

start() {
  (cd "$APP_DIR" && IMAGE_REF="$1" docker compose up -d --no-build "$SERVICE")
}

healthy() {
  local id status deadline=$((SECONDS + HEALTH_TIMEOUT))
  id=$(cd "$APP_DIR" && docker compose ps -q "$SERVICE")
  while ((SECONDS < deadline)); do
    status=$(docker inspect --format '{{.State.Health.Status}}' "$id" 2>/dev/null || echo saknas)
    [[ $status == healthy ]] && return 0
    sleep "$HEALTH_POLL_SECONDS"
  done
  return 1
}

mkdir -p "$STATE_DIR"
exec 9>"$STATE_DIR/lock"
flock -n 9 || { log "en driftsättning pågår redan"; exit 0; }

current=$(cat "$STATE_DIR/current" 2>/dev/null || true)
rejected=$(cat "$STATE_DIR/rejected" 2>/dev/null || true)

latest=$(docker buildx imagetools inspect "$IMAGE:$TAG" --format '{{.Manifest.Digest}}')
[[ $latest == sha256:* ]] || { log "oväntat svar från registret: $latest"; exit 1; }
[[ $latest == "$current" || $latest == "$rejected" ]] && exit 0

ref="$IMAGE@$latest"
log "ny version: $ref"

if ! cosign verify --certificate-identity "$SIGNER_IDENTITY" \
  --certificate-oidc-issuer "$OIDC_ISSUER" "$ref" >/dev/null; then
  echo "$latest" >"$STATE_DIR/rejected"
  notify "signaturen stämmer inte för $ref, startar den inte"
  exit 1
fi

docker pull --quiet "$ref" >/dev/null
start "$ref"
if healthy; then
  echo "$latest" >"$STATE_DIR/current"
  [[ -n $current ]] && echo "$current" >"$STATE_DIR/previous"
  log "körs nu: $ref"
  exit 0
fi

echo "$latest" >"$STATE_DIR/rejected"
if [[ -n $current ]]; then
  start "$IMAGE@$current"
  notify "$ref blev inte frisk inom ${HEALTH_TIMEOUT} s; tillbaka på $IMAGE@$current"
else
  notify "$ref blev inte frisk inom ${HEALTH_TIMEOUT} s, och det finns ingen tidigare version att gå tillbaka till"
fi
exit 1
