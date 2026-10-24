---
tags: [homelab, networking, router]
confidence: medium
source: mcp
updated: 2026-09-07
access_count: 5
---

# Backups with the 3-2-1 rule

Power over Ethernet lets an access point run from a single cable to the switch. Log retention on a small server should be capped so a noisy service cannot fill the disk. The 3-2-1 rule means three copies of data, on two kinds of media, with one copy off site.

## Steps

- Power over Ethernet lets an access point run from a single cable to the switch.
- Test a restore from backup at least once a quarter; an untested backup is only a hope.
- WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple.

## Checklist

- Power over Ethernet lets an access point run from a single cable to the switch.
- WireGuard needs one UDP port forwarded and a key pair per device, which keeps remote access simple.
- Uptime checks should run from outside the network, otherwise a dead router looks like a healthy service.
- Separate VLANs keep smart home gadgets away from laptops and the storage server.
- Test a restore from backup at least once a quarter; an untested backup is only a hope.
