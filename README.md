# XrayProbe

XrayProbe is a small cross-platform CLI for testing Xray configurations. It downloads the official Xray-core binary when needed, runs a configuration through a temporary local SOCKS proxy, and reports the connection’s outbound IP, location, latency, reliability, and quality score.

XrayProbe is unofficial and is not affiliated with or endorsed by [Project X / Xray-core](https://github.com/XTLS/Xray-core). Xray-core is downloaded from its official GitHub releases at runtime; no Xray binary is stored in this repository.

## Install

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/Real-kia/XrayProbe/main/scripts/install.sh | sh
```

On Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/Real-kia/XrayProbe/main/scripts/install.ps1 | iex
```

You can also download a platform archive from the [Releases](https://github.com/Real-kia/XrayProbe/releases) page or install from source:

```sh
go install github.com/Real-kia/XrayProbe/cmd/xrayprobe@latest
```

## Quick start

Test one share link:

```sh
xrayprobe test 'vless://YOUR-UUID@example.com:443?security=tls&type=ws&path=%2F#example'
```

Test a native Xray JSON configuration:

```sh
xrayprobe test ./config.json
```

The JSON input must be a complete Xray configuration. An outbound fragment
from a panel (for example, an object with `protocol`, `settings`, and
`streamSettings` at the top level) needs to be wrapped in an `outbounds` array;
for a VLESS outbound, its server belongs under `settings.vnext[].address` and
its client under `settings.vnext[].users[]`.

Bind Xray's outbound connections to a specific local network interface (for
example, `en0` on macOS or `eth0` on Linux):

```sh
xrayprobe test --interface en0 ./config.json
```

Use `ifconfig` to list interface names. The interface setting is passed to
Xray-core's outbound `sockopt.interface`, so it controls the connection from
Xray to the tested server while the local MCP/CLI process itself remains on
the normal loopback connection.

For tunnel validation, test the public tunnel endpoint and the backend
endpoint separately with the same profile. A direct pass does not prove that
the forwarding path carries application data:

```sh
xrayprobe test --interface en0 --attempts 3 --timeout 15s \
  --no-metadata --format table 'ss://...@PUBLIC_TUNNEL_IP:PORT'
xrayprobe test --interface en0 --attempts 3 --timeout 15s \
  --no-metadata --format table 'ss://...@BACKEND_IP:PORT'
```

If the backend passes but the tunnel fails, first verify that the public port
actually reaches the tunnel listener rather than a stale DNAT/port-forward
rule. Then check the tunnel's forwarded-byte counters and test the backend
from the relay's loopback address as well as its public address; some Xray
deployments accept the public path but stall when reached through loopback.

Test and rank a plain-text or Base64 subscription:

```sh
xrayprobe test https://example.com/subscription.txt --concurrency 4
```

## MCP server for AI clients

XrayProbe includes a local stdio [Model Context Protocol](https://modelcontextprotocol.io/) server. An AI client can call the tools without learning the CLI flags:

* `test_xray_config` tests one share link or JSON config locally or from a configured remote SSH target.
* `test_xray_subscription` tests a subscription and returns a summary, the best config, the top working results, and compact failures, locally or remotely.
* `get_xrayprobe_status` reports the XrayProbe/core cache and server limits.

Start it directly from a shell:

```sh
xrayprobe mcp --allow-path /absolute/path/to/configs \
  --allow-dynamic-targets \
  --interface en0
```

The server communicates over stdin/stdout using MCP. Diagnostics go to stderr. Without a `target`, tests run on the MCP host. A configured target uses non-interactive SSH, automatically installs the matching XrayProbe release in the remote user’s `~/.local/bin`, and lets that remote process download/cache Xray-core. The remote host needs SSH access from the MCP host plus `sh`, `curl`, and `tar` for first-time bootstrap.

`--interface NAME` sets the default interface for local tests. Each test tool
also accepts an optional `interface` field to override that default for one
call. Leave both unset to use the operating system's normal route. When using
a remote `target`, the interface name must exist on the remote machine.

You can either configure friendly aliases once with `--target NAME=SSH_ADDRESS` or enable `--allow-dynamic-targets`. With the dynamic option, the AI can supply a direct SSH target in the tool input, such as `"target": "root@203.0.113.10"`, without changing MCP settings for each server. SSH keys, `~/.ssh/config`, jump hosts, and an SSH agent continue to be managed by the operating system. XrayProbe never stores passwords or private keys. Dynamic targets are opt-in and validated before being passed as an SSH destination.

For a remote test, a local file under `--allow-path` is transferred as content, while an HTTPS subscription URL is fetched from the remote machine. The returned outbound IP and quality metrics therefore describe the selected remote environment.

Remote bootstrap installs the exact tagged XrayProbe release on the target before
running `remote-worker`. Release archive names are lowercase by platform (for
example, `xrayprobe_linux_amd64.tar.gz`); the installer validates the matching
checksum before installation. If bootstrap fails, no probe result is produced;
fix SSH/bootstrap errors before interpreting the config as failed.

Bootstrap fetches `scripts/install.sh` from this repository's `main` branch
over HTTPS and runs it unattended on the remote host (the downloaded release
archive itself is checksum-verified before use). Only point `--target` /
dynamic targets at hosts you trust, the same as any `curl | sh` bootstrap.

By default, MCP file inputs may read only files under the server’s working directory. Add one or more `--allow-path` directories for config folders. Share links, JSON/text values, and HTTPS subscription URLs can be passed directly by tools. File inputs are resolved through symlinks and limited to 10 MiB; subscription decoding is limited to 500 configs by default. Use a dedicated working directory and allow only trusted config directories.

### Codex

Register the binary as a local MCP server. Use an absolute binary and config path:

```sh
codex mcp add xrayprobe -- /absolute/path/to/xrayprobe mcp \
  --allow-path /absolute/path/to/configs \
  --allow-dynamic-targets
```

For a binary installed on `PATH`, first resolve its path with `command -v xrayprobe` and use that absolute path in the registration command.

### Claude Code

```sh
claude mcp add xrayprobe -- /absolute/path/to/xrayprobe mcp \
  --allow-path /absolute/path/to/configs \
  --allow-dynamic-targets
```

### Claude Desktop

Add an entry to Claude Desktop’s MCP configuration, replacing the paths:

```json
{
  "mcpServers": {
    "xrayprobe": {
      "command": "/absolute/path/to/xrayprobe",
      "args": [
        "mcp",
        "--allow-path",
        "/absolute/path/to/configs",
        "--allow-dynamic-targets",
        "--interface",
        "en0"
      ]
    }
  }
}
```

The MCP server is intentionally stdio-only in this release. There is no HTTP listener or OAuth flow. Remote execution uses configured SSH targets or the explicitly enabled dynamic target mode and a fixed XrayProbe worker command; it does not provide general remote shell access or remote config discovery.

The first run automatically downloads and verifies the latest stable Xray-core release. Select an exact release when needed:

```sh
xrayprobe core install v26.3.27
xrayprobe core use v26.3.27
xrayprobe core current
```

## What is measured?

The default health probe performs five HTTPS requests through the selected Xray outbound. It reports success rate, minimum/median/p95 latency, jitter, and an optional IP metadata lookup. The score is intentionally a simple health indicator:

* 50% success rate
* 35% latency, reaching zero at 1,000 ms median latency
* 15% jitter, reaching zero at 250 ms average consecutive difference

Grades are Excellent (85+), Good (70+), Fair (50+), and Poor. Use `--speed` for an opt-in download sample; speed is not part of the default score.

## Useful options

```text
--core-version latest|vX.Y.Z  Select the Xray-core release
--outbound TAG                Select a JSON outbound explicitly
--interface NAME              Bind Xray outbound connections to this interface
--attempts N                  Probe attempts per config (default: 5)
--timeout 10s                 Timeout for each probe request
--concurrency N               Subscription workers (default: 4)
--format table|json|csv       Output format
--output FILE                 Write output to a file
--no-metadata                 Skip IP/location/ASN lookup
--probe-url URL               Use a custom HTTPS health endpoint
--metadata-url URL            Use a custom HTTPS metadata endpoint
--speed                       Run an opt-in download sample
```

For JSON input with multiple outbounds, XrayProbe chooses the first eligible proxy outbound. Use `--outbound TAG` to select one explicitly. Existing user files are never modified; XrayProbe creates a temporary test config and removes it after the process exits.

## Privacy and safety

Probe requests go through the tested configuration. By default, XrayProbe contacts the configured health endpoint and `ipwho.is` for metadata. These services can see the tested outbound IP and request metadata. Use `--no-metadata` and custom endpoints when required.

Share links, UUIDs, passwords, and temporary configs are not printed by normal output. Treat the input itself as secret and avoid shell history when appropriate. Only HTTPS remote subscriptions are accepted by default, and downloads are limited to 10 MiB and 500 configurations.

## Supported links

The first release parses VLESS, VMess, Trojan, and Shadowsocks links with common TCP, WebSocket, gRPC, HTTPUpgrade, XHTTP, TLS, and REALITY options. Native Xray JSON is the escape hatch for advanced or uncommon configurations.

## Development

```sh
go version # Go 1.25 or newer
go test ./...
go vet ./...
go run ./cmd/xrayprobe version
```

Releases are built by GitHub Actions from version tags. The installer regression
check covers platform archive casing and the PowerShell checksum-field parser.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the release workflow.

## License

XrayProbe is released under the MIT License. Xray-core remains a separate project with its own license and distribution terms.
