# Security

## Reporting a vulnerability

Write to **hello@portmole.com** with "security" in the subject. Please do not open a public issue for a
security problem.

Include what you found, how to reproduce it and what an attacker could do
with it. You will get an answer within three working days. We would like to
fix the problem before it is made public, and will tell you when that has
happened; if you want to be credited in the release notes, say so.

## What is in scope

- The client in this repository: the tunnel protocol, the agent, the MCP
  server and the command line.
- The hosted service at `portmole.dev` and `*.portmole.dev`. Its source is
  not public, but it is in scope: reaching another account's tunnel, token or
  request log, signing in as someone else, getting around the plan limits in
  a way that harms other users, or taking the service down with little
  effort.
- The npm packages `portmole` and `@portmole/*`, and the release downloads.

## What is not

- Content served through someone's tunnel. A tunnel shows whatever its owner
  runs locally. To report a tunnel used for phishing or malware, use
  https://portmole.com/abuse.html, not this address.
- A tunnel address being guessable. Addresses are built from the project and
  account names on purpose; a tunnel that should stay private needs
  `basic_auth`.
- Reports that need access to the victim's machine or their stored token.
- Volumetric denial of service.

## Testing

Test against your own account and your own tunnels. Do not read or change
other people's data, and do not run load tests against the hosted service.
