# Security policy

Please do not publish private share links, UUIDs, passwords, or subscription URLs in an issue.

Report security vulnerabilities privately to the repository owner through GitHub rather than opening a public issue. Include a minimal reproduction, affected version, operating system, and the impact. Redact secrets from logs and screenshots.

XrayProbe launches a downloaded network core and executes user-provided configurations. Use only configurations you trust, keep XrayProbe updated, and review a release checksum before installing it through an alternate channel.

The MCP server lets an AI client request tests from the machine running XrayProbe. Run it as a local stdio process, use a dedicated working directory, and pass `--allow-path` only for directories containing trusted configurations. The server does not provide SSH access or remote file discovery, but tested configurations and subscription URLs can still cause outbound network requests.
