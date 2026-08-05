package websocket

import spicestarter "github.com/spice-framework/spice/annotation/sdk/starter"

// Manifest returns WebSocket starter compatibility and review metadata.
func Manifest() spicestarter.Manifest {
	return spicestarter.Must(spicestarter.Spec{
		Schema:    spicestarter.Schema,
		ID:        "github.com/spice-framework/starter-websocket",
		Version:   "0.1.0-dev",
		Module:    "github.com/spice-framework/starter-websocket",
		SpiceAPI:  spicestarter.APIVersion,
		MinimumGo: "1.26",
		License:   "Apache-2.0",
		Review:    "docs/dependency-review.md",
		Activation: spicestarter.Activation{
			Mode: spicestarter.ActivationExplicitConstructor,
			EntryPoints: []spicestarter.EntryPoint{
				{
					Package: "github.com/spice-framework/starter-websocket",
					Symbol:  "NewHandler",
				},
				{
					Package: "github.com/spice-framework/starter-websocket",
					Symbol:  "Dial",
				},
			},
		},
		Capabilities: []string{
			"web.websocket.client",
			"web.websocket.server",
		},
		Dependencies: []spicestarter.Dependency{{
			Module:  "github.com/coder/websocket",
			Version: "v1.8.15",
			License: "ISC",
		}},
	})
}
