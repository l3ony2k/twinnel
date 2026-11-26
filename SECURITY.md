# Security policy

duet runs a root helper and manages the network path of the machine it runs on, so we treat security reports seriously.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting for this repository ("Security" tab → "Report a vulnerability"). Include what you found, how to reproduce it, and the version or commit.

We aim to acknowledge a report within a week. duet is maintained in spare time, so a fix can take longer; we will keep you informed and credit you if you wish.

## Scope

Especially interesting:

- Anything that lets an unprivileged local user make the helper write, download or execute something it should not (the RPC allowlist, peer credential check, kernel download and verification, config validation and rollback).
- Leaks of the mihomo controller secret, subscription URLs or node credentials into logs, history, backups, reports or error messages.
- Ways a composed config can silently bypass the leak guard or the Tailscale coexist settings.

The latest internal review, with findings and their status, is in `docs/security-review.md`.

Out of scope: vulnerabilities in mihomo or Tailscale themselves (report those upstream), and setups where the attacker already has root.

## Supported versions

duet is in early development. Only the latest commit on `main` is supported.
