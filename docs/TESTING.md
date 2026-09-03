# Testing

## Automated

| Command | What runs | Where |
|---|---|---|
| `make test` | Unit tests with the race detector, fake WireGuard device, in-process gRPC/TLS | Every OS, CI matrix |
| `make lint` | gofmt, go vet, golangci-lint | Linux, CI |
| `make integration` | Network-namespace tests in `tests/`: server boot, two-client enrollment, encrypted ping, NAT traversal (restricted/restricted, full-cone/symmetric, symmetric/symmetric, same LAN; needs `nft`), relay over symmetric NATs, relay-to-direct upgrade (needs `conntrack`), relay throughput (needs `iperf3`), policy enforcement end to end (needs `nc`), the nftables ruleset listing (`internal/wg`, needs `nft`) | Linux, root, iproute2, nftables |

The integration tests use the real binary and the WireGuard adapter that
`wg.Open` selects on the host: the kernel module when it loads, else
`wireguard-go` on a TUN device. Run them on a Linux VM with:

```
sudo make integration
```

## Manual checklist for the WireGuard adapters

Run once per adapter when the adapter code changes. Record the result
in the pull request.

### Linux, kernel module (`backend=kernel`)

1. `modprobe wireguard`, then `thawr client up ...` as root.
2. `ip link show thawr0` exists, `ip addr` shows `100.64.x.y/10`.
3. `wg show thawr0` lists the hub and the visible peers with the same
   public keys as `thawr client status`.
4. `thawr client down` removes the interface.

### Linux, userspace (`backend=userspace`)

1. `rmmod wireguard` (or a host without the module) and repeat the steps
   above; `thawr client status` reports `"backend":"userspace"`.

### macOS (`utun`)

1. `sudo thawr client up --interface utun ...`; the log shows the
   assigned `utunN`.
2. `ifconfig utunN` shows the overlay address; `netstat -rn` has a route
   for the overlay prefix over `utunN`.
3. `ping` a peer; `thawr client status` shows a handshake.

### Windows (untested as of spec 003)

1. Place `wintun.dll` next to `thawr.exe`, run an elevated shell.
2. `thawr client up --interface Thawr ...`; `netsh interface ipv4 show
   addresses Thawr` shows the overlay address.
3. `ping` a peer.

If a step fails, the adapter code for that platform in `internal/wg`
(`addr_<os>.go`) is the place to look.

## Manual checklist for NAT traversal (spec 004)

Two devices behind different home routers, server on a public host.

1. `thawr client status` on each device lists its own `endpoints`: a
   LAN address and a reflexive address with the router's public IP;
   `symmetric` is `false` on ordinary routers.
2. The peer shows `path: idle` and `probes: 0` until traffic flows.
3. `thawr client ping <peer>` returns `direct` within 10 s with the
   peer's public `ip:port`; `ping` over the overlay works and the other
   device shows `direct` as well.
4. On a symmetric NAT (many carrier-grade NATs) the device reports
   `symmetric: true`; two such devices end at `unreachable` until the
   relay (spec 005).
5. Two devices on the same LAN pick the LAN address as the path
   endpoint even though both have reflexive candidates.
6. The admin UI's "Show" under a peer lists the paths it reported.

## Manual checklist for the relay (spec 005)

1. Put one device behind a symmetric NAT (a phone hotspot usually is
   one) and `thawr client ping <peer>`: the JSON says `relay`, `thawr
   client status` shows `relay.connected: true`, `relay.peers: 1`, and
   `ping`/SSH over the overlay work.
2. `/api/v1/status` on the server counts `relay.sessions`, `frames` and
   `bytes` growing; with `relay.max_bytes_per_second` set low, `drops`
   grows and the transfer slows.
3. Restart the server: `status` shows `relay.connected` false, then true
   again within seconds; traffic resumes without `client down`.
4. Move the relayed device to an ordinary network: within about a
   minute the peer reads `direct` and `relay.peers` drops to 0; the
   relay connection closes five minutes later.
5. `curl -k https://server/relay` without a token answers 401; the
   server log never contains payload bytes.

## Manual checklist for the policy (spec 006)

1. Start with no policy file: `thawr client status` on every device
   lists no peers; `thawr admin policy show` reports the empty
   default-deny policy.
2. Write a rule opening one port from one user to another, run `thawr
   admin policy reload`: both devices list each other within 5 s, the
   destination's `status` shows `filter.rules: 1`.
3. `nc` to the allowed port connects; to another port it times out and
   `filter.drops` grows on the destination; a reply to a connection
   the destination opened itself passes.
4. With kernel WireGuard `nft list table inet thawr` shows the chain
   with policy drop and one rule per policy entry; with `wireguard-go`
   the same behaviour comes from the userspace filter.
5. `thawr admin policy check bad.yaml` names the rule index and field;
   a reload of a broken file answers with the errors and `show` still
   prints the previous hash.
6. A member cannot create a token carrying a tag that `tagOwners` does
   not grant them (403 in the UI); an admin always can.
