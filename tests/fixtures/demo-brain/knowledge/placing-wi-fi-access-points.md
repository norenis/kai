---
tags: [homelab, networking, router, backup]
confidence: low
source: conversation
updated: 2026-09-20
access_count: 8
---

# Placing Wi-Fi access points

Separate VLANs keep smart home gadgets away from laptops and the storage server. WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple. A local DNS resolver caches lookups and can block tracking domains for every device on the network. Keep the router admin page reachable only from the management VLAN.

## Steps

- The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site.
- WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple.
- Log retention on a small server should be capped so a noisy service cannot fill the disk.
- A local DNS resolver caches lookups and can block tracking domains for every device on the network.
