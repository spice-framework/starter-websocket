# Starter WebSocket implementation contract

This repository owns the independently versioned WebSocket integration for
Spice. Work directly on local `main` in bounded commits. Fetch before editing
and immediately before pushing; never overwrite unexpected remote work.

Go 1.26.5 is mandatory. Every product change must preserve fail-closed TLS,
authentication and origin policy, bounded messages and sessions, caller-owned
contexts and lifecycle, graceful idempotent close, and payload-free diagnostics.
Construction must never start listeners, open connections, install globals,
read ambient configuration, or perform hidden network access.

Add positive and failure-path tests, update public documentation, run
`make verify` on the exact commit tree, and push only a green commit.

Release-rehearsal work must preserve the exact `spice-dev` tool version
authorized by the root `go.mod`, invoke its full package path, and render the
same inert plan twice with workspace and network resolution disabled in vendor
mode. The protected central workflow is the sole production builder. An
unsigned rehearsal must never manufacture signatures or key material.
