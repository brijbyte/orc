#!/bin/sh
# Provision a tailnet-only Hetzner VM running the OpenCode web UI.
# Usage: deploy-opencode/provision.sh [name]
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
name=${1:-opencode}
case "$name" in *[!a-zA-Z0-9-]*|'') echo "opencode: invalid server name: $name" >&2; exit 1 ;; esac

envval() {
    [ -f "$1" ] || return 1
    sed -n "s/^$2=//p" "$1" | head -1 | sed 's/^"//;s/"$//'
}
b64() { base64 < "$1" | tr -d '\n'; }

f="$here/.env"
: "${HCLOUD_TOKEN:=$(envval "$f" HCLOUD_TOKEN || true)}"
: "${TS_AUTHKEY:=$(envval "$f" TS_AUTHKEY || true)}"
: "${TS_API_KEY:=$(envval "$f" TS_API_KEY || true)}"
: "${GIT_BOT_NAME:=$(envval "$f" GIT_BOT_NAME || true)}"
: "${GIT_BOT_EMAIL:=$(envval "$f" GIT_BOT_EMAIL || true)}"
: "${TELEGRAM_BOT_TOKEN:=$(envval "$f" TELEGRAM_BOT_TOKEN || true)}"
: "${TELEGRAM_CHAT_ID:=$(envval "$f" TELEGRAM_CHAT_ID || true)}"
: "${NTFY_URL:=$(envval "$f" NTFY_URL || true)}"
: "${NTFY_TOKEN:=$(envval "$f" NTFY_TOKEN || true)}"
: "${GIT_BOT_NAME:=opencode-bot}"
: "${GIT_BOT_EMAIL:=opencode-bot@users.noreply.github.com}"

[ -n "${HCLOUD_TOKEN:-}" ] || { echo 'opencode: HCLOUD_TOKEN not set' >&2; exit 1; }
[ -n "${TS_AUTHKEY:-}" ] || { echo 'opencode: TS_AUTHKEY not set' >&2; exit 1; }
if [ -n "$TELEGRAM_BOT_TOKEN$TELEGRAM_CHAT_ID" ] && { [ -z "$TELEGRAM_BOT_TOKEN" ] || [ -z "$TELEGRAM_CHAT_ID" ]; }; then
    echo 'opencode: Telegram requires TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID' >&2
    exit 1
fi
[ -n "$TELEGRAM_BOT_TOKEN" ] || [ -n "$NTFY_URL" ] || {
    echo 'opencode: configure Telegram or ntfy notifications' >&2
    exit 1
}

type=${OPENCODE_VM_TYPE:-cx23}
location=${OPENCODE_VM_LOCATION:-fsn1}
image=${OPENCODE_VM_IMAGE:-debian-13}
pubkey=${OPENCODE_SSH_PUBKEY:-$HOME/.ssh/id_ed25519.pub}
gitkey=${OPENCODE_GIT_KEY:-$here/git_ed25519}
opencode_auth=${OPENCODE_AUTH_FILE:-$here/opencode-auth.json}
agy_auth=${AGY_AUTH_FILE:-$here/agy-auth-token}
github_auth=${GITHUB_AUTH_FILE:-$here/github-auth.yml}
[ -f "$pubkey" ] || { echo "opencode: no SSH public key at $pubkey" >&2; exit 1; }
[ -f "$gitkey" ] || { echo "opencode: no git key at $gitkey" >&2; exit 1; }
[ -f "$opencode_auth" ] || { echo "opencode: no auth file at $opencode_auth" >&2; exit 1; }
[ -f "$agy_auth" ] || { echo "opencode: no agy token at $agy_auth" >&2; exit 1; }
[ -f "$github_auth" ] || { echo "opencode: no GitHub auth file at $github_auth" >&2; exit 1; }

api=https://api.hetzner.cloud/v1
hcurl() {
    method=$1; path=$2; shift 2
    curl -fsS -X "$method" "$api$path" \
        -H "Authorization: Bearer $HCLOUD_TOKEN" \
        -H 'Content-Type: application/json' "$@"
}
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

keyname=opencode-key
key_id=$(hcurl GET /ssh_keys | PUBKEY="$pubkey" python3 -c '
import json, os, sys
public_key = open(os.environ["PUBKEY"]).read().strip()
for key in json.load(sys.stdin)["ssh_keys"]:
    if key["public_key"] == public_key:
        print(key["id"])
        break')
if [ -z "$key_id" ]; then
    key_id=$(hcurl POST /ssh_keys -d "$(python3 - "$keyname" "$pubkey" <<'PY'
import json, sys
print(json.dumps({"name": sys.argv[1], "public_key": open(sys.argv[2]).read().strip()}))
PY
)" | json 'd["ssh_key"]["id"]')
fi

if hcurl GET "/servers?name=$name" | json 'len(d["servers"])' | grep -qv '^0$'; then
    echo "opencode: server '$name' already exists" >&2
    exit 1
fi

if [ -n "$TS_API_KEY" ]; then
    curl -fsS -u "$TS_API_KEY:" 'https://api.tailscale.com/api/v2/tailnet/-/devices' |
        NAME="$name" python3 -c '
import datetime, json, os, sys
cutoff = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=5)
for dev in json.load(sys.stdin)["devices"]:
    seen = datetime.datetime.fromisoformat(dev["lastSeen"].replace("Z", "+00:00"))
    if dev["name"].split(".")[0] == os.environ["NAME"] and seen < cutoff:
        print(dev["id"])' |
        while read -r dev; do
            echo "opencode: removing stale tailnet node $dev named '$name'"
            curl -fsS -u "$TS_API_KEY:" -X DELETE "https://api.tailscale.com/api/v2/device/$dev"
        done
fi

password=$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')
notify_config=$(python3 - "$TELEGRAM_BOT_TOKEN" "$TELEGRAM_CHAT_ID" "$NTFY_URL" "$NTFY_TOKEN" <<'PY'
import shlex, sys
for key, value in zip(("TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "NTFY_URL", "NTFY_TOKEN"), sys.argv[1:]):
    print(f"{key}={shlex.quote(value)}")
PY
)
server_config="OPENCODE_SERVER_USERNAME=opencode
OPENCODE_SERVER_PASSWORD=$password"

agents_b64=$(b64 "$here/AGENTS.md")
opencode_config_b64=$(b64 "$here/opencode.jsonc")
opencode_auth_b64=$(b64 "$opencode_auth")
github_notifications_agent_b64=$(b64 "$here/agents/github-notifications.md")
github_notifications_policy_b64=$(b64 "$here/policies/github-notifications.md")
github_auth_b64=$(b64 "$github_auth")
agy_config_b64=$(b64 "$here/agy-settings.json")
agy_auth_b64=$(b64 "$agy_auth")
agy_wrapper_b64=$(b64 "$here/agy.sh")
agy_skill_b64=$(b64 "$here/skills/agy-search/SKILL.md")
notify_script_b64=$(b64 "$here/notify-owner.sh")
notify_config_b64=$(printf '%s\n' "$notify_config" | base64 | tr -d '\n')
server_config_b64=$(printf '%s\n' "$server_config" | base64 | tr -d '\n')
ts_authkey_b64=$(printf '%s' "$TS_AUTHKEY" | base64 | tr -d '\n')
gitkey_b64=$(b64 "$gitkey")
git_name_b64=$(printf '%s' "$GIT_BOT_NAME" | base64 | tr -d '\n')
git_email_b64=$(printf '%s' "$GIT_BOT_EMAIL" | base64 | tr -d '\n')
userdata=$(sed \
    -e "s|__SERVER_NAME__|$name|g" \
    -e "s|__TS_AUTHKEY_B64__|$ts_authkey_b64|" \
    -e "s|__AGENTS_B64__|$agents_b64|" \
    -e "s|__OPENCODE_CONFIG_B64__|$opencode_config_b64|" \
    -e "s|__OPENCODE_AUTH_B64__|$opencode_auth_b64|" \
    -e "s|__GITHUB_NOTIFICATIONS_AGENT_B64__|$github_notifications_agent_b64|" \
    -e "s|__GITHUB_NOTIFICATIONS_POLICY_B64__|$github_notifications_policy_b64|" \
    -e "s|__GITHUB_AUTH_B64__|$github_auth_b64|" \
    -e "s|__AGY_CONFIG_B64__|$agy_config_b64|" \
    -e "s|__AGY_AUTH_B64__|$agy_auth_b64|" \
    -e "s|__AGY_WRAPPER_B64__|$agy_wrapper_b64|" \
    -e "s|__AGY_SKILL_B64__|$agy_skill_b64|" \
    -e "s|__NOTIFY_SCRIPT_B64__|$notify_script_b64|" \
    -e "s|__NOTIFY_CONFIG_B64__|$notify_config_b64|" \
    -e "s|__SERVER_CONFIG_B64__|$server_config_b64|" \
    -e "s|__GIT_KEY_B64__|$gitkey_b64|" \
    -e "s|__GIT_NAME_B64__|$git_name_b64|" \
    -e "s|__GIT_EMAIL_B64__|$git_email_b64|" \
    "$here/cloud-init.yaml")

echo "opencode: creating $name ($type, $location, $image)"
payload=$(python3 - "$name" "$type" "$location" "$image" "$key_id" "$userdata" <<'PY'
import json, sys
print(json.dumps({
    "name": sys.argv[1], "server_type": sys.argv[2], "location": sys.argv[3],
    "image": sys.argv[4], "ssh_keys": [int(sys.argv[5])], "user_data": sys.argv[6],
}))
PY
)
resp=$(printf '%s' "$payload" | hcurl POST /servers -d @-)
server_id=$(echo "$resp" | json 'd["server"]["id"]')
ip=$(echo "$resp" | json 'd["server"]["public_net"]["ipv4"]["ip"]')

printf 'opencode: server %s booting' "$server_id"
while :; do
    status=$(hcurl GET "/servers/$server_id" | json 'd["server"]["status"]')
    [ "$status" = running ] && break
    printf .
    sleep 3
done
printf ' running (%s)\n' "$ip"

cat <<EOF

Cloud-init is installing OpenCode, agy, and the web service. This takes a few minutes.
Public SSH closes once the firewall comes up; use the tailnet afterward.

Web UI:   https://$name.<tailnet>.ts.net
Username: opencode
Password: $password

Next steps:
  watch setup:  ssh root@$name tail -f /var/log/cloud-init-output.log
  open a shell: ssh opencode@$name
  test notify: ssh opencode@$name notify-owner 'VM ready' 'Notification works'
EOF
