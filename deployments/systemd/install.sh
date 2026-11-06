# path: deployments/systemd/install.sh
#!/usr/bin/env bash
# Forge CI - Bare-Metal / Air-Gapped Systemd Installer
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: This installer script must be executed as root (e.g. sudo bash install.sh)." >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "================================================================"
echo " Installing Forge CI Systemd Services"
echo "================================================================"

# 1. Create system user and group 'forge'
if ! id -u forge &>/dev/null; then
    echo "==> Creating system user and group 'forge'..."
    useradd --system --shell /bin/false --home-dir /var/lib/forge forge
else
    echo "==> User 'forge' already exists."
fi

# Ensure user 'forge' is in the 'docker' group if docker is installed
if getent group docker &>/dev/null; then
    echo "==> Adding 'forge' user to 'docker' group..."
    usermod -aG docker forge
fi

# 2. Setup state and configuration directories
mkdir -p /var/lib/forge
chown -R forge:forge /var/lib/forge
chmod 0750 /var/lib/forge

mkdir -p /etc/forge
if [ ! -f /etc/forge/forge.env ]; then
    echo "==> Installing default environment file to /etc/forge/forge.env..."
    cp "${SCRIPT_DIR}/forge.env" /etc/forge/forge.env
    chmod 0600 /etc/forge/forge.env
    chown forge:forge /etc/forge/forge.env
else
    echo "==> Configuration /etc/forge/forge.env already exists, preserving."
fi

# 3. Install binaries into /usr/local/bin
echo "==> Installing binaries..."
BIN_SRC=""
if [ -f "${ROOT_DIR}/bin/forge-api" ]; then
    BIN_SRC="${ROOT_DIR}/bin"
elif [ -f "${SCRIPT_DIR}/../../dist/bin/forge-api" ]; then
    BIN_SRC="${SCRIPT_DIR}/../../dist/bin"
elif [ -f "./bin/forge-api" ]; then
    BIN_SRC="./bin"
elif [ -f "./forge-api" ]; then
    BIN_SRC="."
fi

if [ -n "${BIN_SRC}" ] && [ -f "${BIN_SRC}/forge-api" ] && [ -f "${BIN_SRC}/forge-runner" ]; then
    install -m 0755 "${BIN_SRC}/forge-api" /usr/local/bin/forge-api
    install -m 0755 "${BIN_SRC}/forge-runner" /usr/local/bin/forge-runner
    echo "==> Binaries installed to /usr/local/bin/forge-api and /usr/local/bin/forge-runner"
else
    echo "Notice: Pre-compiled binaries not found in local path."
    echo "Please copy 'forge-api' and 'forge-runner' to /usr/local/bin/ and make them executable (chmod 0755)."
fi

# 4. Install systemd service units
echo "==> Installing systemd service units..."
cp "${SCRIPT_DIR}/forge-api.service" /etc/systemd/system/forge-api.service
cp "${SCRIPT_DIR}/forge-runner.service" /etc/systemd/system/forge-runner.service
chmod 0644 /etc/systemd/system/forge-api.service /etc/systemd/system/forge-runner.service

# 5. Reload systemd daemon
echo "==> Reloading systemd daemon..."
systemctl daemon-reload

echo "================================================================"
echo " Forge CI services installed successfully!"
echo ""
echo " Next Steps:"
echo " 1. Edit /etc/forge/forge.env with your production credentials."
echo " 2. Enable services to start at boot:"
echo "    sudo systemctl enable forge-api forge-runner"
echo " 3. Start the services:"
echo "    sudo systemctl start forge-api forge-runner"
echo " 4. Inspect status and logs:"
echo "    sudo systemctl status forge-api forge-runner"
echo "    sudo journalctl -u forge-api -f"
echo "================================================================"

