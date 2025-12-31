# Repository Guidelines

## Project Structure & Module Organization
- `main.go` wires CLI flags and starts the DNS server lifecycle.
- `dns_server.go` contains the DNS server implementation and request handling.
- `proxmox.go` manages Proxmox instance discovery (containers/VMs) and IP lookup.
- `install.sh` installs the prebuilt binary and registers the systemd service.
- `README.md` documents usage and operational requirements.

## Build, Test, and Development Commands
- `go build -o proxmox-dns-server` builds the binary locally.
- `go run . -zone example.local` runs the server from source.
- `./proxmox-dns-server -zone example.local -port 5353` runs the compiled binary.
- `go test ./...` runs the test suite (currently minimal/none, but keep it green).

## Coding Style & Naming Conventions
- Use standard Go formatting (`gofmt`); tabs for indentation.
- Follow Go naming conventions: `CamelCase` for exported identifiers, `lowerCamel` for unexported.
- Keep log messages short and consistent; include identifiers like VMID or name where helpful.

## Testing Guidelines
- There are no dedicated unit tests yet. If you add new logic (parsing, filtering, API calls), add tests and run `go test ./...`.
- Prefer table-driven tests in `*_test.go` files in the same package.

## Commit & Pull Request Guidelines
- Commit messages in this repo are short, imperative summaries (e.g., “Add support for …”, “Fix …”). Avoid long prefixes or scopes unless necessary.
- PRs should include a brief description, the motivation, and testing notes (even if “not run”).
- Update `README.md` if user-facing flags, requirements, or runtime behavior change.

## Security & Configuration Notes
- The server typically runs on a Proxmox VE node and needs permissions to query instances.
- Use `-ip-prefix` to constrain which IPv4 ranges are served.
- If behavior depends on Proxmox APIs/commands, document required permissions clearly.
