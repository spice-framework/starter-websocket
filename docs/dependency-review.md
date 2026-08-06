# Dependency review: coder/websocket

- Decision: approved for the independently versioned
  `github.com/spice-framework/starter-websocket` module.
- Version: `github.com/coder/websocket` v1.8.15.
- Upstream: <https://github.com/coder/websocket>.
- License: ISC; retained with the mechanically vendored source.
- Maintenance: Coder actively maintains the v1 line. The selected release was
  published June 15, 2026. The library implements RFC 6455, passes the Autobahn
  test suite, supports context-aware I/O, close handshakes, concurrent writes,
  ping/pong, subprotocols, same-origin checks, and RFC 7692 compression.
- Dependency scope: the module is small and adds no transitive modules. Spice
  does not adopt a separate HTTP server, router, codec, or message protocol.
- Security: inbound TLS, authentication, and same-origin checks are required by
  default. Anonymous access is explicit. Plaintext clients and servers are
  restricted to loopback even when insecure development is enabled.
  Cross-origin patterns are explicit, and any-origin behavior additionally
  requires authentication. Outbound URLs reject embedded credentials,
  fragments, missing ports, and non-loopback plaintext. TLS verification
  cannot be disabled. Authorization is a dedicated bounded value; generic
  headers cannot smuggle authorization, cookies, or WebSocket control headers.
  Redirects are rejected so credentials cannot move to a different endpoint.
  Messages, connections, subprotocols, origins, headers, compression
  thresholds, handshake timeouts, and close timeouts are bounded.
- Cancellation: reads, writes, ping, dial, and close use caller contexts. Dial
  also applies an explicit bounded handshake timeout. Native read cancellation
  closes the connection and is documented. Close performs one bounded,
  idempotent handshake and force-closes on cancellation.
- Observability: the Spice seam exposes direction, subprotocol, outcome, and
  duration only. It cannot receive headers, URLs, peer addresses, close reasons,
  or payload bytes. Authentication failures and peer close errors do not retain
  credential diagnostics or close-reason text.
- Configuration: `NewHandler` performs no network work and returns an ordinary
  `http.Handler`. `Dial` is the only outbound connection operation. No package
  import starts a listener, installs a global registry, or downloads modules.
- Verification: race-enabled tests exercise real local TLS and certificate
  validation, authenticated concurrent sessions, repeated cleanup, handshake
  timeout, text exchange, subprotocol negotiation, ping/read coordination,
  insecure non-loopback rejection, cross-origin rejection, capacity exhaustion,
  size limits, close cancellation, payload-safe diagnostics, observation, and
  defensive configuration.

Primary references:

- <https://github.com/coder/websocket/releases/tag/v1.8.15>
- <https://pkg.go.dev/github.com/coder/websocket@v1.8.15>
- <https://github.com/coder/websocket/blob/v1.8.15/LICENSE.txt>

## Build-only dependencies: Spice release tools

- Decision: approved as the repository-authorized release signer, renderer,
  and independent verifier.
- Signer version: `github.com/spice-framework/development`
  `v0.0.0-20260806132124-4c308d1b9fda`.
- Signer tool: `github.com/spice-framework/development/cmd/spice-dev` through the
  standard Go `tool` directive; invocations always use the full package path.
- Verifier version: `github.com/spice-framework/toolchain`
  `v0.0.0-20260806133530-71211498297c`.
- Verifier tool:
  `github.com/spice-framework/toolchain/cmd/spice-library-release-verify`.
- License: Apache-2.0, with its notice retained in `vendor`.
- Runtime scope: none. Product packages do not import the development module,
  and released applications acquire no runtime dependency on it.
- Dependency graph: the tool participates in normal Go minimal-version
  selection. That build-time coupling is accepted and visible in `go.mod`,
  `go.sum`, and `vendor/modules.txt`; no parallel tool registry is introduced.
- Integrity and network behavior: the exact pseudo-version is pinned and
  checksummed. Release parity runs with `GOWORK=off`, `GOPROXY=off`,
  `GOTOOLCHAIN=local`, and `GOFLAGS=-mod=vendor`, so it cannot select an ambient
  checkout, upgrade itself, or download dependencies.
- Security: the trusted native tool reads the exact committed Git graph and
  writes only to caller-supplied temporary output directories. The rehearsal
  emits no signatures or signing material.
- Maintenance: the protected central workflow owns production. The retained
  local builder remains only as the dual-builder parity oracle and is not
  removed by this cutover.
