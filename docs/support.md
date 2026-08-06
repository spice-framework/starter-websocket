# Support policy

| Contract | Current development support |
|---|---|
| Go | Exactly 1.26.5 for development and release verification |
| Spice minimum/current | Exact versions in [`spice-compatibility.json`](../spice-compatibility.json) |
| WebSocket implementation | `github.com/coder/websocket` v1.8.15 |
| Operating systems | Windows, Linux, and macOS |
| Architectures | amd64 and arm64 compilation through public Go APIs |
| Server transport | Caller-owned TLS-enabled `http.Server`; loopback-only plaintext opt-in |
| Authentication | Required callback or explicit anonymous mode |
| Client transport | Verified `wss`; loopback-only `ws` opt-in |
| Release signer | `github.com/spice-framework/development/cmd/spice-dev` at `v0.0.0-20260806121906-963bb6676069` |
| Independent verifier | `github.com/spice-framework/toolchain/cmd/spice-library-release-verify` at `v0.0.0-20260806054457-a83d9b58034c` |

`spice-compatibility.json` is the sole compatibility boundary source. Its
minimum must equal the exact direct Spice requirement in `go.mod`; its current
value is a forward-compatibility endpoint rather than a moving branch. The
repository gate verifies both boundaries using isolated alternate modfiles,
exact MVS selection, vet, and shuffled race tests without modifying product or
module files. A release may raise the minimum only through an intentional
module and compatibility-contract change with green minimum/current evidence.

The starter supports complete text and binary WebSocket messages, subprotocols,
ping, bounded no-context-takeover compression, graceful close, and payload-free
session observations. Application message codecs, reconnection, delivery
guarantees, session stores, authorization policy, routers, and HTTP/TLS server
ownership remain outside this starter and must be composed explicitly.

Release artifacts are produced only from an exact tagged commit under the
contract in [`releasing.md`](releasing.md). A compromised or missing signing
secret fails a production release; it never falls back to unsigned output.

The pinned central tool renders unsigned rehearsal candidates only. Windows
and Linux CI compare them with the retained builder under vendor-only offline
resolution; the retained command remains the signed production authority.
