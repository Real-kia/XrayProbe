# Security policy

Please do not publish private share links, UUIDs, passwords, or subscription URLs in an issue.

Report security vulnerabilities privately to the repository owner through GitHub rather than opening a public issue. Include a minimal reproduction, affected version, operating system, and the impact. Redact secrets from logs and screenshots.

XrayProbe launches a downloaded network core and executes user-provided configurations. Use only configurations you trust, keep XrayProbe updated, and review a release checksum before installing it through an alternate channel.

The MCP server lets an AI client request tests from the machine running XrayProbe or from explicitly configured SSH probe targets. Run it as a local stdio process, use a dedicated working directory, and pass `--allow-path` only for directories containing trusted configurations. Configure only hosts you intend to permit with `--target NAME=SSH_ADDRESS`; use a dedicated low-privilege SSH account and existing host-key/agent controls. Remote bootstrap executes the fixed XrayProbe installer and worker command, not arbitrary tool-provided shell text. Tested configurations and subscription URLs can still cause outbound network requests from the selected environment.
