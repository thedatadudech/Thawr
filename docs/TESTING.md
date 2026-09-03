# Testing

## Automated

| Command | What runs | Where |
|---|---|---|
| `make test` | Unit tests with the race detector, fake WireGuard device, in-process gRPC/TLS | Every OS, CI matrix |
| `make lint` | gofmt, go vet, golangci-lint | Linux, CI |
| `make integration` | Network-namespace tests in `tests/`: server boot, two-client enrollment, encrypted ping | Linux, root, iproute2 |

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
