---
tags: [homelab, networking, backup]
confidence: low
source: conversation
updated: 2026-09-18
access_count: 3
---

# Reverse proxy and TLS certificates

Test a restore from backup at least once a quarter; an untested backup is only a hope. Access points work best mounted high and in the middle of the space they cover, away from metal shelves. Power over Ethernet lets an access point run from a single cable to the switch. A reverse proxy terminates TLS and routes each hostname to the right internal service.

## Pitfalls

Give servers static leases in DHCP instead of hard coding addresses on each machine. Power over Ethernet lets an access point run from a single cable to the switch. A local DNS resolver caches lookups and can block tracking domains for every device on the network. A reverse proxy terminates TLS and routes each hostname to the right internal service. Log retention on a small server should be capped so a noisy service cannot fill the disk. Keep the router admin page reachable only from the management VLAN.

A local DNS resolver caches lookups and can block tracking domains for every device on the network. Access points work best mounted high and in the middle of the space they cover, away from metal shelves. Separate VLANs keep smart home gadgets away from laptops and the storage server. Certificates from an ACME provider renew automatically when the proxy can answer the DNS challenge.

## Checklist

- Log retention on a small server should be capped so a noisy service cannot fill the disk.
- Give servers static leases in DHCP instead of hard coding addresses on each machine.
- A reverse proxy terminates TLS and routes each hostname to the right internal service.

## Steps

- The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site.
- Power over Ethernet lets an access point run from a single cable to the switch.
- Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service.
- Keep the router admin page reachable only from the management VLAN.
- Give servers static leases in DHCP instead of hard coding addresses on each machine.
