---
tags: [homelab, networking, backup]
confidence: medium
source: import
updated: 2026-08-15
---

# Monitoring uptime at home

Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service. The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site. WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple.

## Pitfalls

Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge. A reverse proxy terminates TLS and routes each hostname to the right internal service. Keep the router admin page reachable only from the management VLAN. The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site. Log retention on a small server should be capped so a noisy service cannot fill the disk. A local DNS resolver caches lookups and can block tracking domains for every device on the network.

Keep the router admin page reachable only from the management VLAN. Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service. Give servers static leases in DHCP instead of hard coding addresses on each machine. Test a restore from backup at least once a quarter; an untested backup is only a hope.

## Steps

- A reverse proxy terminates TLS and routes each hostname to the right internal service.
- A local DNS resolver caches lookups and can block tracking domains for every device on the network.
- Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service.
- Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.
