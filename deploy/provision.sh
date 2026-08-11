#!/bin/sh
# Provision a Hetzner Cloud VM running the orc web service on your tailnet.
#
#   deploy/provision.sh [name]
#
# Reads HCLOUD_TOKEN and TS_AUTHKEY from the environment or deploy/.env.
# Optional overrides: ORC_VM_TYPE (cx23), ORC_VM_LOCATION (fsn1),
# ORC_VM_IMAGE (debian-13), ORC_SSH_PUBKEY (~/.ssh/id_ed25519.pub),
# ORC_GIT_KEY (deploy/orc_ed25519).
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
name=${1:-orc}

# envval FILE KEY: first KEY=value line, value trimmed of quotes.
envval() {
    [ -f "$1" ] || return 1
    sed -n "s/^$2=//p" "$1" | head -1 | sed 's/^"//;s/"$//'
}

f="$here/.env"
: "${HCLOUD_TOKEN:=$(envval "$f" HCLOUD_TOKEN || true)}"
: "${TS_AUTHKEY:=$(envval "$f" TS_AUTHKEY || true)}"
: "${TS_API_KEY:=$(envval "$f" TS_API_KEY || true)}"
: "${GIT_BOT_NAME:=$(envval "$f" GIT_BOT_NAME || true)}"
: "${GIT_BOT_EMAIL:=$(envval "$f" GIT_BOT_EMAIL || true)}"
: "${GIT_BOT_NAME:=orc-bot}"
: "${GIT_BOT_EMAIL:=orc-bot@users.noreply.github.com}"
gitkey=${ORC_GIT_KEY:-$here/orc_ed25519}
[ -n "${HCLOUD_TOKEN:-}" ] || { echo "orc: HCLOUD_TOKEN not set" >&2; exit 1; }
[ -n "${TS_AUTHKEY:-}" ] || { echo "orc: TS_AUTHKEY not set" >&2; exit 1; }

type=${ORC_VM_TYPE:-cx23}
location=${ORC_VM_LOCATION:-fsn1}
image=${ORC_VM_IMAGE:-debian-13}
pubkey=${ORC_SSH_PUBKEY:-$HOME/.ssh/id_ed25519.pub}
[ -f "$pubkey" ] || { echo "orc: no SSH public key at $pubkey" >&2; exit 1; }

api="https://api.hetzner.cloud/v1"
hcurl() {
    method=$1; path=$2; shift 2
    curl -fsS -X "$method" "$api$path" \
        -H "Authorization: Bearer $HCLOUD_TOKEN" \
        -H "Content-Type: application/json" "$@"
}
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

# idempotent SSH key upload (name orc-key); conflict means it is already there
keyname="orc-key"
hcurl POST /ssh_keys -d "$(python3 - "$keyname" "$pubkey" <<'EOF'
import json, sys
print(json.dumps({"name": sys.argv[1], "public_key": open(sys.argv[2]).read().strip()}))
EOF
)" >/dev/null 2>&1 || true

if hcurl GET "/servers?name=$name" | json 'len(d["servers"])' | grep -qv '^0$'; then
    echo "orc: server '$name' already exists" >&2
    exit 1
fi

# A dead tailnet node holding this name would push the new VM to "$name-1".
# With TS_API_KEY, drop same-name devices that are offline; without it, a
# stale node means renaming by hand in the admin console.
if [ -n "${TS_API_KEY:-}" ]; then
    curl -fsS -u "$TS_API_KEY:" "https://api.tailscale.com/api/v2/tailnet/-/devices" |
        python3 -c "
import json, sys, datetime
cutoff = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=5)
for dev in json.load(sys.stdin)['devices']:
    seen = datetime.datetime.fromisoformat(dev['lastSeen'].replace('Z', '+00:00'))
    if dev['name'].split('.')[0] == '$name' and seen < cutoff:
        print(dev['id'])" |
        while read -r dev; do
            echo "orc: removing stale tailnet node $dev named '$name'"
            curl -fsS -u "$TS_API_KEY:" -X DELETE "https://api.tailscale.com/api/v2/device/$dev"
        done
fi

[ -f "$gitkey" ] || { echo "orc: no git key at $gitkey (set ORC_GIT_KEY)" >&2; exit 1; }
agents_b64=$(base64 < "$here/AGENTS.md" | tr -d '\n')
gitkey_b64=$(base64 < "$gitkey" | tr -d '\n')
userdata=$(sed -e "s|__TS_AUTHKEY__|$TS_AUTHKEY|" \
    -e "s|__SERVER_NAME__|$name|" \
    -e "s|__AGENTS_B64__|$agents_b64|" \
    -e "s|__GIT_KEY_B64__|$gitkey_b64|" \
    -e "s|__GIT_BOT_NAME__|$GIT_BOT_NAME|" \
    -e "s|__GIT_BOT_EMAIL__|$GIT_BOT_EMAIL|" "$here/cloud-init.yaml")

echo "orc: creating $name ($type, $location, $image)"
payload=$(python3 - "$name" "$type" "$location" "$image" "$keyname" "$userdata" <<'EOF'
import json, sys
print(json.dumps({
    "name": sys.argv[1], "server_type": sys.argv[2], "location": sys.argv[3],
    "image": sys.argv[4], "ssh_keys": [sys.argv[5]], "user_data": sys.argv[6],
}))
EOF
)
resp=$(printf '%s' "$payload" | hcurl POST /servers -d @-)
server_id=$(echo "$resp" | json 'd["server"]["id"]')
ip=$(echo "$resp" | json 'd["server"]["public_net"]["ipv4"]["ip"]')

printf "orc: server %s booting" "$server_id"
while :; do
    status=$(hcurl GET "/servers/$server_id" | json 'd["server"]["status"]')
    [ "$status" = "running" ] && break
    printf .
    sleep 3
done
echo " running ($ip)"

cat <<EOF

cloud-init now installs tailscale + the orc service (a few minutes).
public SSH ($ip) closes once the firewall comes up mid-boot; use the
tailnet for everything after the node appears.
next steps:
  1. watch it:        ssh root@$name tail -f /var/log/cloud-init-output.log
  2. open the UI:     http://$name.<tailnet>.ts.net
     (https on the same name once the ACME cert lands; see orc-cert.service)
  3. web password:    ssh orc@$name orc password --rotate
  4. codex sign-in:   ssh orc@$name orc --login
     (or copy your local auth.json to ~/.config/orc/auth.json on the VM)
EOF
