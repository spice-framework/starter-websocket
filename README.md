# Spice WebSocket starter

`github.com/spice-framework/starter-websocket` is the independently versioned,
opt-in RFC 6455 client and server integration for Spice applications. It wraps
`github.com/coder/websocket` with fail-closed transport, authentication, origin,
resource-limit, lifecycle, and diagnostic policy while leaving HTTP servers,
TLS certificates, credentials, routing, contexts, and shutdown with the
application.

## Secure server boundary

`NewHandler` constructs an ordinary `http.Handler`; it never starts a listener,
opens a connection, reads ambient configuration, or registers global state.
TLS and authentication are required by default. Anonymous access and plaintext
loopback development are separate, explicit choices.

```go
handler, err := websocket.NewHandler(websocket.ServerConfig{
	Authenticate: func(ctx context.Context, request *http.Request) error {
		return authorizer.Check(ctx, request.Header.Get("Authorization"))
	},
	OriginPatterns:  []string{"app.example.com"},
	Subprotocols:    []string{"orders.v1"},
	MaxMessageBytes: 64 << 10,
	MaxConnections:  128,
}, serveSession)
if err != nil {
	return err
}
server.Handler = handler
```

The caller must serve this handler through a TLS-enabled `http.Server`.
`AllowInsecure` accepts only loopback requests. `AllowAnyOrigin` requires a
non-nil authenticator. Authorization failures return a generic response and
never expose the authenticator's error.

## Explicit client connection

`Dial` is the only outbound network operation. It requires either an explicit
authorization value or `AllowAnonymous`, requires `wss` outside loopback, clones
caller TLS and header state defensively, rejects redirects, and applies a
bounded handshake timeout.

```go
connection, response, cleanup, err := websocket.Dial(ctx, websocket.ClientConfig{
	URL:              "wss://events.example.com:443/orders",
	Authorization:    "Bearer " + token,
	Subprotocols:     []string{"orders.v1"},
	MaxMessageBytes:  64 << 10,
	HandshakeTimeout: 5 * time.Second,
}, observer)
if err != nil {
	return err
}
defer response.Body.Close()
defer cleanup(shutdownContext)
```

The application owns the returned connection and cleanup. Reads, writes, ping,
dial, and close use caller contexts. Cleanup performs one graceful close and is
idempotent; cancellation force-closes the socket. `Observer` receives only
direction, negotiated subprotocol, outcome, and duration. It never receives
URLs, headers, credentials, peer addresses, close reasons, or message payloads.
Peer close errors retain the status code but redact the close reason.

## Manifest, compatibility, and migration

`Manifest` declares the explicit `NewHandler` and `Dial` entrypoints. Importing
the package alone has no runtime effect. Existing consumers migrate from
`github.com/spice-framework/spice/starter/websocket` to
`github.com/spice-framework/starter-websocket`; constructor semantics remain
ordinary Go and require no runtime Spice compiler.

Development and verification require exactly Go 1.26.5. The machine-readable
[`spice-compatibility.json`](spice-compatibility.json) records the exact minimum
and current Spice core lines. The repository gate proves both lines, real local
TLS behavior, race safety, an 85% coverage floor, security analysis,
reproducible vendor contents, and offline builds.

```text
make check
make compatibility
make release-rehearsal
make verify
make verify-release
```

Release rehearsal runs the exact `spice-dev` tool authorized by `go.mod`
twice from one inert plan, entirely from `vendor` with network and workspace
resolution disabled. It requires byte-identical outputs, canonical checksums,
central-renderer SPDX provenance, and no rehearsal signatures on Windows and
Linux.

See [`docs/dependency-review.md`](docs/dependency-review.md) and
[`docs/support.md`](docs/support.md).

## Releases

Each version tag is an ordinary Go module release. The repository also builds
an exact-commit source archive, committed-graph SPDX 2.3 SBOM, SHA-256
checksums, and an Ed25519 signature/public key without an external release
build system. Production mode requires a clean checkout, exact tag, and
protected signing key; an explicit unsigned rehearsal is available for local
proof. See [`docs/releasing.md`](docs/releasing.md) for the artifact and trust
contract.
The protected central workflow is the sole release authority. It validates the
candidate without credentials, renders and signs with immutable trusted code,
authenticates the result with an independent verifier, and publishes only after
separate protected approvals.
