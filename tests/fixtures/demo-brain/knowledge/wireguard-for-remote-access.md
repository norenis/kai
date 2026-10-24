---
tags: [homelab, networking, router, dns]
confidence: low
source: conversation
updated: 2026-08-16
access_count: 3
---

# WireGuard for remote access

Access points work best mounted high and in the middle of the space they cover, away from metal shelves. Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service. A reverse proxy terminates TLS and routes each hostname to the right internal service. Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.

## Steps

- Test a restore from backup at least once a quarter; an untested backup is only a hope.
- Power over Ethernet lets an access point run from a single cable to the switch.
- Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.

## Why it matters

WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple. Test a restore from backup at least once a quarter; an untested backup is only a hope. Keep the router admin page reachable only from the management VLAN. A reverse proxy terminates TLS and routes each hostname to the right internal service. Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.

A reverse proxy terminates TLS and routes each hostname to the right internal service. Give servers static leases in DHCP instead of hard coding addresses on each machine. WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple.

## Notes

WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple. Give servers static leases in DHCP instead of hard coding addresses on each machine. Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge. Keep the router admin page reachable only from the management VLAN. A reverse proxy terminates TLS and routes each hostname to the right internal service. Access points work best mounted high and in the middle of the space they cover, away from metal shelves.

Test a restore from backup at least once a quarter; an untested backup is only a hope. WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple. A local DNS resolver caches lookups and can block tracking domains for every device on the network. Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.
