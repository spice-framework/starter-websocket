// Package schema defines stable wire-schema identities shared by Spice core,
// Toolchain, editor, workspace, and agent adapters.
package schema

const (
	// ProjectModel is the complete Toolchain-facing Project Model schema.
	ProjectModel = "spice.project-model/v1alpha1"
	// AgentProjectModel is the canonical-path-free agent projection schema.
	AgentProjectModel = "spice.project-model.agent/v1alpha1"
	// WorkspaceProtocol is the local session workspace protocol schema.
	WorkspaceProtocol = "spice.workspace/v1alpha1"
	// ModuleMetadata is the spice.module.json integer schema version.
	ModuleMetadata = 1
)
