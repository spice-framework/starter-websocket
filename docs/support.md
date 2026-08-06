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
| Release signer | `github.com/spice-framework/development/cmd/spice-dev` at `v0.0.0-20260806132124-4c308d1b9fda` |
| Independent verifier | `github.com/spice-framework/toolchain/cmd/spice-library-release-verify` at `v0.0.0-20260806133530-71211498297c` |
| Public trust anchor | [`security/release/ed25519-public.pem`](../security/release/ed25519-public.pem), SHA-256 `0602e904ecc1e0da5cf09e415e66d47347d6ab638909d3cc475fd2f47cf69d67` |

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

The pinned central signer and independent verifier are the protected production
path. Windows and Linux CI still compare the central renderer with the retained
builder under vendor-only offline resolution; the retained command is a parity
oracle only.

The committed public trust anchor is reviewed verification material. Its
fingerprint is the SHA-256 digest of the DER SubjectPublicKeyInfo bytes. The
anchor does not establish that a matching private signing secret, protected
release environments, a version tag, or a published release exists.
