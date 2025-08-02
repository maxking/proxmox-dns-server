# Proxmox DNS Server

An authoritative DNS server for Proxmox VE that resolves DNS names based on VM and LXC container names and IDs.

## Features

- Resolves DNS queries for Proxmox VMs and LXC containers
- Supports both ID-based and name-based resolution
- Filters to IPv4 addresses starting with 192.168.x.x
- Runs as an authoritative DNS server for a specified zone
- Automatic refresh of instance information every 30 seconds

## Usage

```bash
# Build the application
go build -o proxmox-dns-server

# Run with zone specification
./proxmox-dns-server -zone p01.araj.me

# Run on custom port
./proxmox-dns-server -zone p01.araj.me -port 5353

# Use configuration file
./proxmox-dns-server -config config.json
```

## Configuration File

Create a JSON configuration file:

```json
{
  "zone": "p01.araj.me",
  "port": "53"
}
```

## DNS Resolution Examples

For zone `p01.araj.me`:

- `102.p01.araj.me` → IP address of container/VM with ID 102
- `mycontainer.p01.araj.me` → IP address of container/VM named "mycontainer"

## Requirements

- Must run on the Proxmox VE node
- Requires permission to execute `pct` and `qm` commands
- Only resolves IPv4 addresses starting with 192.168.x.x

## Permissions

The application needs to run with sufficient privileges to execute Proxmox commands:
- `pct list` and `pct exec` for LXC containers
- `qm list` and `qm guest cmd` for VMs