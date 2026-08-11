#!/bin/sh
# Apply safe cloud-init package updates to an existing VM: deploy/update.sh [name]
# The VM must be reachable as root over the tailnet.
set -eu

name=${1:-orc}

printf 'orc: updating %s\n' "$name"
ssh root@"$name" sh -s <<'EOF'
set -eu

# Keep this list in sync with the applicable package setup in cloud-init.yaml.
curl -fsSL https://deb.nodesource.com/setup_current.x | bash -
apt-get install -y nodejs
npm install --global corepack@latest
EOF

echo "orc: $name updated"
