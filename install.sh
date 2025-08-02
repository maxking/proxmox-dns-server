#!/bin/bash

set -e

REPO_URL="git.araj.me/araj/proxmox-dns-server"
SERVICE_NAME="proxmox-dns-server"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"

usage() {
    echo "Usage: $0 -z <zone> [-p <port>]"
    echo "Example: $0 -z p01.araj.me -p 53"
    exit 1
}

ZONE=""
PORT="53"

while getopts "z:p:h" opt; do
    case ${opt} in
        z )
            ZONE=$OPTARG
            ;;
        p )
            PORT=$OPTARG
            ;;
        h )
            usage
            ;;
        \? )
            echo "Invalid option: $OPTARG" 1>&2
            usage
            ;;
    esac
done

if [ -z "$ZONE" ]; then
    echo "Error: Zone is required"
    usage
fi

if [[ $EUID -ne 0 ]]; then
   echo "This script must be run as root"
   exit 1
fi

echo "Installing Proxmox DNS Server..."
echo "Zone: $ZONE"
echo "Port: $PORT"

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

echo "Creating systemd service..."
cat > "$SERVICE_DIR/$SERVICE_NAME.service" << EOF
[Unit]
Description=Proxmox DNS Server
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/proxmox-dns-server -zone $ZONE -port $PORT
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