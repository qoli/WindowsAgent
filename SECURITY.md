# Security Policy

## Supported versions

Security updates currently target the latest revision on the default branch.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's private vulnerability
reporting or security-advisory flow for this repository. Do not open a public
issue containing exploit details, credentials, private screenshots, memory
dumps, or host information.

## Deployment warning

The current screenshot API has no authentication or TLS and listens on
`0.0.0.0:8787` by default. Restrict reachability to a trusted LAN or private
overlay network. Do not expose it directly to the public Internet.

The installer does not alter Windows Firewall and does not create a traditional
Windows service. The agent must run with the signed-in user's interactive token.
