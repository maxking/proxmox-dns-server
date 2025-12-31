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

# Run using the Proxmox API
./proxmox-dns-server -zone p01.araj.me \
  -api-url https://proxmox:8006 \
  -api-token-id root@pam!dns \
  -api-token-secret <secret>

# Run on custom port
./proxmox-dns-server -zone p01.araj.me -port 5353 \
  -api-url https://proxmox:8006 \
  -api-token-id root@pam!dns \
  -api-token-secret <secret>

# Read the API token secret from the environment
PVE_API_TOKEN_SECRET="<secret>" ./proxmox-dns-server -zone p01.araj.me \
  -api-url https://proxmox:8006 \
  -api-token-id root@pam!dns
```


## DNS Resolution Examples

For zone `p01.araj.me`:

- `102.p01.araj.me` → IP address of container/VM with ID 102
- `mycontainer.p01.araj.me` → IP address of container/VM named "mycontainer"

## Requirements

- Provide `-api-url`, `-api-token-id`, and `-api-token-secret`
  so the server can query the Proxmox API.
- VM IP detection relies on the QEMU guest agent being installed and running.
- Only resolves IPv4 addresses starting with 192.168.x.x. This currently because
  that's how I use it. If you feel like using this and would like a configuration
  for this, open a issue or even better, a PR. We might be also able to support
  like a configuration of sorts to define the interface.

## Permissions

The application needs to run with sufficient privileges to query instance data:
- API mode requires a Proxmox API token that can read:
  - `GET /cluster/resources?type=vm`
  - `GET /nodes/<node>/lxc/<id>/config`
  - `GET /nodes/<node>/qemu/<id>/agent/network-get-interfaces`

## Token Creation

On a Proxmox node, create an API token and capture the secret output:

```bash
pveum user token add root@pam dns --privsep 0 --comment "proxmox-dns-server"
```

The command prints the token secret once. Store it securely and pass it via
`-api-token-secret` or the `PVE_API_TOKEN_SECRET` environment variable (do not commit it).
