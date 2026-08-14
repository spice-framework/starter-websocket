module github.com/spice-framework/starter-websocket

go 1.26.0

toolchain go1.26.6

require (
	github.com/coder/websocket v1.8.15
	github.com/spice-framework/spice v0.1.0-preview.4.0.20260814014712-5f535e696300
)

require (
	github.com/spice-framework/development v0.0.0-20260806132124-4c308d1b9fda // indirect
	github.com/spice-framework/toolchain v0.0.0-20260806133530-71211498297c // indirect
	golang.org/x/mod v0.38.0 // indirect
)

tool (
	github.com/spice-framework/development/cmd/spice-dev
	github.com/spice-framework/toolchain/cmd/spice-library-release-verify
)
