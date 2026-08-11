#!/bin/sh
# Delete the Hetzner VM created by provision.sh: deploy/destroy.sh [name]
# With TS_API_KEY, the tailnet node goes too; without it, clean it up in the
# Tailscale admin console.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
name=${1:-orc}

envval() {
    [ -f "$1" ] || return 1
    sed -n "s/^$2=//p" "$1" | head -1 | sed 's/^"//;s/"$//'
}
f="$here/.env"
: "${HCLOUD_TOKEN:=$(envval "$f" HCLOUD_TOKEN || true)}"
: "${TS_API_KEY:=$(envval "$f" TS_API_KEY || true)}"
[ -n "${HCLOUD_TOKEN:-}" ] || { echo "orc: HCLOUD_TOKEN not set" >&2; exit 1; }

api="https://api.hetzner.cloud/v1"
hcurl() {
    method=$1; path=$2; shift 2
    curl -fsS -X "$method" "$api$path" \
        -H "Authorization: Bearer $HCLOUD_TOKEN" "$@"
}
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

server_id=$(hcurl GET "/servers?name=$name" \
    | json 'd["servers"][0]["id"] if d["servers"] else ""')
[ -n "$server_id" ] || { echo "orc: no server named '$name'" >&2; exit 1; }

printf "orc: delete server %s (%s)? [y/N] " "$name" "$server_id"
read -r answer
[ "$answer" = "y" ] || exit 1
hcurl DELETE "/servers/$server_id" >/dev/null
echo "orc: deleted"

if [ -n "${TS_API_KEY:-}" ]; then
    curl -fsS -u "$TS_API_KEY:" "https://api.tailscale.com/api/v2/tailnet/-/devices" |
        python3 -c "
import json, sys
for dev in json.load(sys.stdin)['devices']:
    if dev['name'].split('.')[0] == '$name':
        print(dev['id'])" |
        while read -r dev; do
            curl -fsS -u "$TS_API_KEY:" -X DELETE "https://api.tailscale.com/api/v2/device/$dev"
            echo "orc: removed tailnet node $dev"
        done
fi
