# Security policy

## About this project

Much of XrayProbe's code was written with AI assistance. That means it can contain mistakes an experienced reviewer would catch, and it hasn't had the scrutiny that comes from years of production use. The project is open source specifically so that isn't the end of the story: issues, pull requests, and independent security review are welcome and genuinely useful here, not just a formality.

If you find a bug, a rough edge, or something that looks unsafe, please say so — through an issue for general problems or privately for anything security-sensitive (see below).

Please do not publish private share links, UUIDs, passwords, or subscription URLs in an issue.

Report security vulnerabilities privately to the repository owner through GitHub rather than opening a public issue. Include a minimal reproduction, affected version, operating system, and the impact. Redact secrets from logs and screenshots.

XrayProbe launches a downloaded network core and executes user-provided configurations. Use only configurations you trust, keep XrayProbe updated, and review a release checksum before installing it through an alternate channel.

The MCP server lets an AI client request tests from the machine running XrayProbe or from SSH probe targets. Run it as a local stdio process, use a dedicated working directory, and pass `--allow-path` only for directories containing trusted configurations. Configure fixed hosts with `--target NAME=SSH_ADDRESS`, or explicitly opt in to direct tool-supplied SSH destinations with `--allow-dynamic-targets`. Use a dedicated low-privilege SSH account and existing host-key/agent controls. Remote bootstrap executes the fixed XrayProbe installer and worker command, not arbitrary tool-provided shell text. Tested configurations and subscription URLs can still cause outbound network requests from the selected environment.
