# Security Policy

Smart Wi-Fi Manager is intended to run on trusted companion-computer networks.
The dashboard binds to `127.0.0.1:9080` by default. Expose it to a LAN/VPN only
when the surrounding network is trusted.

## Current Posture

- Wi-Fi passwords must not be committed to public repositories.
- Dashboard access should be limited by loopback, VPN, firewall, or SSH tunnel.
- Logs and APIs should expose password state such as `stored` or `missing`, not
  raw passwords.

## Deferred Hardening

Future work should add:

- optional dashboard login similar to MDS
- bearer-token protection for mutation APIs
- CIDR allowlists for GCS, NetBird, admin LAN, and field laptop subnets
- Caddy/reverse-proxy guidance for serving Smart Wi-Fi Manager beside MDS
- SSH/CLI recovery steps if auth configuration locks out an operator

Report security issues privately to `p30planets@gmail.com`.

