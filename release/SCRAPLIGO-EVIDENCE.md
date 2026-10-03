# ScrapliGo v1.4.2 integration evidence — karvi v0.25.0

The authoritative module declares:

```text
github.com/scrapli/scrapligo v1.4.2
```

Only `internal/adapters/scrapligov1` may import that module
(`scripts/verify-release.sh` fails on any other import, which is the whole
proof of the boundary). The provider registers the stable implementation ID
`scrapligo-v1` plus version metadata for `karvi --version` in every build;
no build tag is involved.

The executable is built from the committed `go.mod`, `go.sum`, and `vendor/`
(`vendor/modules.txt` names `github.com/scrapli/scrapligo v1.4.2`), with Go
1.27.1 and no network. `evidence/go-version-m.txt` is `go version -m` of the
shipped executables and names the module and its version;
`evidence/version.txt` lists both transports. The sums in `go.sum` are the
ones recorded from the module proxy when the modules were first fetched, and
`go mod verify` passes in `evidence/release-qualification.log`.

scrapligo-v1 contributes the driver and channel; the connection is karvi's own
x/crypto transport with the host-key policy in the handshake, and the session
logic is karvi's device session shared with the `system` transport
([`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](../docs/TRANSPORT-DRIVER-ARCHITECTURE.md)).
The parity suite (`scripts/native-smoke-test.sh`, in both verifiers) runs every
case as `command` and `run` over both transports against the fake IOS XE device
built from the tree and compares the records path by path.

None of this is device qualification: the Catalyst 9300 and ISR 4451-X matrix of
[`docs/CISCO-IOSXE-QUALIFICATION.md`](../docs/CISCO-IOSXE-QUALIFICATION.md) is
run on this executable next.
