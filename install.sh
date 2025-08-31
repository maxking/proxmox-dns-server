#!/bin/bash

set -e

SERVICE_NAME="proxmox-dns-server"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
CONFIG_DIR="/etc"
CONFIG_FILE="$CONFIG_DIR/$SERVICE_NAME.json"

if [[ $EUID -ne 0 ]]; then
   echo "This script must be run as root"
   exit 1
fi

echo "Installing Proxmox DNS Server..."

echo "Fetching latest release information..."

RELEASE_JSON=$(curl -s "https://git.araj.me/api/v1/repos/maxking/proxmox-dns-server/releases/latest")
LATEST_RELEASE=$(echo "$RELEASE_JSON" | jq -r '.tag_name')
DOWNLOAD_URL=$(echo "$RELEASE_JSON" | jq -r '.assets[] | select(.name | contains("linux-amd64")) | .browser_download_url')

if [ -z "$LATEST_RELEASE" ] || [ -z "$DOWNLOAD_URL" ]; then
    echo "Error: Could not fetch latest release information"
    exit 1
fi

echo "Latest release: $LATEST_RELEASE"

echo "Downloading binary..."
curl -L -o /tmp/proxmox-dns-server "$DOWNLOAD_URL"

if [ ! -f /tmp/proxmox-dns-server ]; then
    echo "Error: Failed to download binary"
    exit 1
fi

echo "Installing binary to $INSTALL_DIR..."
chmod +x /tmp/proxmox-dns-server
mv /tmp/proxmox-dns-server "$INSTALL_DIR/"

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Creating default configuration file at $CONFIG_FILE..."
    cat > "$CONFIG_FILE" << EOF
{
  "server": {
    "zone": "pve.example.com",
    "port": "53",
    "interface": "",
    "upstream": "1.1.1.1:53",
    "ip_prefix": "192.168.",
    "refresh_interval": 30,
    "debug": false
  },
  "proxmox": {
    "api_endpoint": "https://localhost:8006/api2/json",
    "username": "root@pam",
    "password": "your-password",
    "node": "pve",
    "insecure_tls": true
  }
}
EOF
    echo "Default configuration file created. Please edit $CONFIG_FILE with your settings."
else
    echo "Configuration file already exists at $CONFIG_FILE, skipping creation."
fi

echo "Creating systemd service..."

cat > "$SERVICE_DIR/$SERVICE_NAME.service" << EOF
[Unit]
Description=Proxmox DNS Server
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/proxmox-dns-server -config $CONFIG_FILE
Restart=always
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF

echo "Reloading systemd daemon..."
systemctl daemon-reload

echo "Enabling and starting service..."
systemctl enable "$SERVICE_NAME"
systemctl start "$SERVICE_NAME"

echo "Installation complete!"
echo "Service status:"
systemctl status "$SERVICE_NAME" --no-pager -l

echo ""
echo "To check logs: journalctl -u $SERVICE_NAME -f"
echo "To stop service: systemctl stop $SERVICE_NAME"
echo "To restart service: systemctl restart $SERVICE_NAME"
