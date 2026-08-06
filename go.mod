module github.com/spice-framework/starter-websocket

go 1.26.0

toolchain go1.26.5

require (
	github.com/coder/websocket v1.8.15
	github.com/spice-framework/spice v0.0.0-20260805222830-a2ecd56df246
)

require (
	github.com/spice-framework/development v0.0.0-20260806052122-9025218a91c0 // indirect
	github.com/spice-framework/toolchain v0.0.0-20260806054457-a83d9b58034c // indirect
	golang.org/x/mod v0.38.0 // indirect
)

tool (
	github.com/spice-framework/development/cmd/spice-dev
	github.com/spice-framework/toolchain/cmd/spice-library-release-verify
)
