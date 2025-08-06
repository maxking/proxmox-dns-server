# Proxmox DNS Server

An authoritative DNS server for Proxmox VE that resolves DNS names based on VM and LXC container names and IDs.

I use it to give DNS names to my containers/VMs to set CNAME records for my services, when the IP address changes, it is automatically reflected without having to manually change my service's DNS records.

## Features

- Resolves DNS queries for Proxmox VMs and LXC containers
- Supports both ID-based and name-based resolution
- Filters to IPv4 addresses starting with 192.168.x.x
- Runs as an authoritative DNS server for a specified zone
- Automatic refresh of instance information every 30 seconds

## Installation

```
wget https://git.araj.me/maxking/proxmox-dns-server/raw/branch/master/install.sh
chmod +x install.sh
./install.sh -p 5353 -z p01.araj.me
```

Use the right zone `p01.araj.me` or whatever prefix you want.

## Usage

```bash
# Build the application
go build -o proxmox-dns-server

# Run with zone specification
./proxmox-dns-server -zone p01.araj.me

# Run on custom port
./proxmox-dns-server -zone p01.araj.me -port 5353
```


## DNS Resolution Examples

For zone `p01.araj.me`:

- `102.p01.araj.me` → IP address of container/VM with ID 102
- `mycontainer.p01.araj.me` → IP address of container/VM named "mycontainer"

## Requirements

- Must run on the Proxmox VE node. This can be relaxed at some
  point if we can figure out how to get the IP addresses of
  containers. VMs IPs are available over API but from what I can
  find, I couldn't easily get a container's IP.
- Requires permission to execute `pct` and `qm` commands so we
  can get the IP address.
- Only resolves IPv4 addresses starting with 192.168.x.x. This currently because
  that's how I use it. If you feel like using this and would like a configuration
  for this, open a issue or even better, a PR. We might be also able to support
  like a configuration of sorts to define the interface.

## Permissions

The application needs to run with sufficient privileges to execute Proxmox commands:
- `pct list` and `pct exec` for LXC containers
- `qm list` and `qm guest cmd` for VMs