//go:build spice_config

package spice

import "github.com/spice-framework/spice/project"

var Build = project.Build{
	Kind: project.StarterKind,
	Dependencies: project.Dependencies{
		project.Library("github.com/coder/websocket", "v1.8.15"),
		project.BuildTool("github.com/spice-framework/toolchain", "v0.0.0-20260806133530-71211498297c"),
	},
}
