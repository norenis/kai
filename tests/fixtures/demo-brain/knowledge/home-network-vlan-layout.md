---
tags: [homelab, networking, wifi, backup, dns]
confidence: medium
source: import
updated: 2026-08-30
access_count: 13
---

# Home network VLAN layout

A local DNS resolver caches lookups and can block tracking domains for every device on the network. Test a restore from backup at least once a quarter; an untested backup is only a hope. Access points work best mounted high and in the middle of the space they cover, away from metal shelves.

## Open questions

A local DNS resolver caches lookups and can block tracking domains for every device on the network. The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site. WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple. Test a restore from backup at least once a quarter; an untested backup is only a hope. Separate VLANs keep smart home gadgets away from laptops and the storage server. Keep the router admin page reachable only from the management VLAN.

Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service. A reverse proxy terminates TLS and routes each hostname to the right internal service.
