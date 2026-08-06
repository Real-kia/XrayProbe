# XrayProbe

XrayProbe is a small cross-platform CLI for testing Xray configurations. It downloads the official Xray-core binary when needed, runs a configuration through a temporary local SOCKS proxy, and reports the connection’s outbound IP, location, latency, reliability, and quality score.

XrayProbe is unofficial and is not affiliated with or endorsed by [Project X / Xray-core](https://github.com/XTLS/Xray-core). Xray-core is downloaded from its official GitHub releases at runtime; no Xray binary is stored in this repository.

## Install

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/KiaTheRandomGuy/XrayProbe/main/scripts/install.sh | sh
```

On Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/KiaTheRandomGuy/XrayProbe/main/scripts/install.ps1 | iex
```

You can also download a platform archive from the [Releases](https://github.com/KiaTheRandomGuy/XrayProbe/releases) page or install from source:

```sh
go install github.com/KiaTheRandomGuy/XrayProbe/cmd/xrayprobe@latest
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

Test and rank a plain-text or Base64 subscription:

```sh
xrayprobe test https://example.com/subscription.txt --concurrency 4
```

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
go test ./...
go vet ./...
go run ./cmd/xrayprobe version
```

Releases are built by GitHub Actions from version tags. See [CONTRIBUTING.md](CONTRIBUTING.md) for the release workflow.

## License

XrayProbe is released under the MIT License. Xray-core remains a separate project with its own license and distribution terms.
