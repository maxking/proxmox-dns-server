# Proxmox DNS Server

An authoritative DNS server for Proxmox VE that resolves DNS names based on VM and LXC container names and IDs.

It gives DNS names to containers/VMs, so when the IP address changes, it is automatically reflected.

## Features

- Resolves DNS queries for Proxmox VMs and LXC containers by ID or name.
- Forwards other DNS queries to an upstream server.
- Automatic refresh of Proxmox instance information.
- Configurable via a JSON file.

## Installation

1.  Download the latest release from the [releases page](https://git.araj.me/maxking/proxmox-dns-server/releases).
2.  Create a configuration file `/etc/proxmox-dns-server.json`:
    ```json
    {
      "server": {
        "zone": "pve.example.com",
        "port": "53",
        "interface": "eth0",
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
    ```
3.  Create a systemd service file `/etc/systemd/system/proxmox-dns-server.service`:
    ```ini
    [Unit]
    Description=Proxmox DNS Server
    After=network.target
    Wants=network.target

    [Service]
    Type=simple
    ExecStart=/usr/local/bin/proxmox-dns-server -config /etc/proxmox-dns-server.json
    Restart=always
    RestartSec=5
    User=root
    Group=root

    [Install]
    WantedBy=multi-user.target
    ```
4.  Reload systemd, enable and start the service:
    ```bash
    systemctl daemon-reload
    systemctl enable proxmox-dns-server
    systemctl start proxmox-dns-server
    ```

## Usage

The server is configured via a JSON file. The default path is `/etc/proxmox-dns-server.json`, but a different path can be specified with the `-config` flag.

### Configuration Options

#### `server`

-   `zone`: The DNS zone for which the server is authoritative (e.g., "pve.example.com").
-   `port`: The port to listen on (default: "53").
-   `interface`: The network interface to bind to. If empty, binds to all interfaces.
-   `upstream`: The upstream DNS server to forward queries to (e.g., "1.1.1.1:53").
-   `ip_prefix`: The IP prefix to filter Proxmox instances by (e.g., "192.168.").
-   `refresh_interval`: The interval in seconds to refresh Proxmox instance data (default: 30).
-   `debug`: Enable debug logging (default: false).

#### `proxmox`

-   `api_endpoint`: The Proxmox API endpoint (e.g., "https://localhost:8006/api2/json").
-   `username`: The Proxmox username.
-   `password`: The Proxmox password.
-   `api_token` and `api_secret`: Alternatively, use an API token and secret for authentication.
-   `node`: The Proxmox node to query.
-   `insecure_tls`: Whether to skip TLS certificate verification (default: false).

### DNS Resolution Examples

For a server configured with `zone: "pve.example.com"`:

-   A query for `102.pve.example.com` will resolve to the IP address of the container/VM with ID 102.
-   A query for `my-vm.pve.example.com` will resolve to the IP address of the container/VM named "my-vm".
-   A query for `google.com` will be forwarded to the upstream DNS server.

## Requirements

-   The server must run on a machine that can access the Proxmox API.
-   The Proxmox user needs sufficient permissions to query VM and container information.
