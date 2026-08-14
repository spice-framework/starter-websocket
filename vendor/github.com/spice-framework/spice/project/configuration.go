// Package project defines the declarative, statically decoded contracts used
// by Spice project configuration and project-model metadata.
//
// Files such as settings.spice.go and build.spice.go are valid Go source, but
// the Spice Toolchain parses their declarations without executing package
// initialization, functions, or constructor bodies.
package project

// Kind identifies the artifact a Spice project builds.
type Kind string

const (
	// Application identifies an executable Spice application.
	Application Kind = "application"
	// LibraryKind identifies a reusable Spice-aware Go library.
	LibraryKind Kind = "library"
	// StarterKind identifies an opt-in Spice starter module.
	StarterKind Kind = "starter"
	// ToolKind identifies a build or compiler tool.
	ToolKind Kind = "tool"
)

// Settings defines one project's identity and build-wide policy.
type Settings struct {
	Name             string
	Module           string
	Toolchain        Toolchain
	Projects         IncludedProjects
	DependencyPolicy DependencyPolicy
}

// Toolchain pins the Go and Spice lines used to interpret a project.
type Toolchain struct {
	Go    string
	Spice string
}

// IncludedProjects is the ordered set of projects participating in a
// multi-project build.
type IncludedProjects []IncludedProject

// IncludedProject names one project rooted at a project-relative directory.
type IncludedProject struct {
	Name      string
	Directory string
}

// Include declares one included project.
func Include(name, directory string) IncludedProject {
	return IncludedProject{Name: name, Directory: directory}
}

// DependencyVerification identifies how strictly declared dependency intent
// must agree with the canonical Go module graph.
type DependencyVerification string

const (
	// Strict requires dependency intent and the Go module graph to agree.
	Strict DependencyVerification = "strict"
)

// DependencyPolicy defines build-wide dependency provenance policy.
type DependencyPolicy struct {
	Verification       DependencyVerification
	ApprovedRegistries []string
	ApprovedProxies    []string
	AllowedModules     []string
	DeniedModules      []string
}

// Build defines what a project builds and consumes.
type Build struct {
	Kind            Kind
	Dependencies    Dependencies
	Plugins         Plugins
	Targets         Targets
	Generators      Generators
	StyleExceptions StyleExceptions
	Views           ViewOverrides
	Packaging       Packaging
}

// DependencyKind identifies a declarative dependency category.
type DependencyKind string

const (
	// DependencyStarter identifies an official or cataloged Spice starter.
	DependencyStarter DependencyKind = "starter"
	// DependencyLibrary identifies an ordinary Go module dependency.
	DependencyLibrary DependencyKind = "library"
	// DependencyTool identifies a Go build or compiler tool dependency.
	DependencyTool DependencyKind = "tool"
)

// DependencyScope identifies the source set that consumes a dependency.
type DependencyScope string

const (
	// ScopeMain makes a dependency available to production source.
	ScopeMain DependencyScope = "main"
	// ScopeTest makes a dependency available only to test source.
	ScopeTest DependencyScope = "test"
	// ScopeBuild makes a dependency available only to build tooling.
	ScopeBuild DependencyScope = "build"
)

// Dependency records human-authored dependency intent. Go modules remain the
// canonical resolver and integrity mechanism.
type Dependency struct {
	Kind    DependencyKind
	Scope   DependencyScope
	Name    string
	Module  string
	Version string
}

// Dependencies is the ordered dependency intent declared by a build.
type Dependencies []Dependency

// Starter declares one named Spice starter for production source.
func Starter(name string) Dependency {
	return Dependency{Kind: DependencyStarter, Scope: ScopeMain, Name: name}
}

// Library declares one versioned ordinary Go module for production source.
func Library(module, version string) Dependency {
	return Dependency{
		Kind:    DependencyLibrary,
		Scope:   ScopeMain,
		Module:  module,
		Version: version,
	}
}

// TestLibrary declares one versioned ordinary Go module for test source.
func TestLibrary(module, version string) Dependency {
	return Dependency{
		Kind:    DependencyLibrary,
		Scope:   ScopeTest,
		Module:  module,
		Version: version,
	}
}

// BuildTool declares one versioned Go tool used only while building.
func BuildTool(module, version string) Dependency {
	return Dependency{
		Kind:    DependencyTool,
		Scope:   ScopeBuild,
		Module:  module,
		Version: version,
	}
}

// PluginKind identifies one statically recognized build plugin contract.
type PluginKind string

const (
	// PluginApplication selects the standard application build conventions.
	PluginApplication PluginKind = "application"
	// PluginCompiler selects a versioned compiler plugin module.
	PluginCompiler PluginKind = "compiler"
)

// Plugin declares one build plugin without executing plugin code during
// project discovery.
type Plugin struct {
	Kind    PluginKind
	Module  string
	Version string
}

// Plugins is the ordered set of build plugins.
type Plugins []Plugin

// ApplicationPlugin selects the standard Spice application build plugin.
func ApplicationPlugin() Plugin {
	return Plugin{Kind: PluginApplication}
}

// CompilerPlugin declares one versioned compiler plugin module.
func CompilerPlugin(module, version string) Plugin {
	return Plugin{Kind: PluginCompiler, Module: module, Version: version}
}

// Target identifies one buildable Go package.
type Target struct {
	Name      string
	Package   string
	Generated string
}

// Targets is the ordered set of build targets.
type Targets []Target

// ApplicationTarget declares one application command package and its
// generated package import path.
func ApplicationTarget(name, packagePath, generatedPackage string) Target {
	return Target{
		Name:      name,
		Package:   packagePath,
		Generated: generatedPackage,
	}
}

// Generator declares one versioned code generator selected by the build.
type Generator struct {
	Module  string
	Version string
}

// Generators is the ordered set of code generators.
type Generators []Generator

// CodeGenerator declares one versioned Go code generator.
func CodeGenerator(module, version string) Generator {
	return Generator{Module: module, Version: version}
}

// StyleExceptionKind identifies one narrowly reviewable style-policy escape
// hatch.
type StyleExceptionKind string

const (
	// StylePackageFunction allows one exact package-level function.
	StylePackageFunction StyleExceptionKind = "package-function"
	// StylePackageVariable allows one exact package-level variable and type.
	StylePackageVariable StyleExceptionKind = "package-variable"
	// StylePublicRoute classifies one exact route as intentionally public.
	StylePublicRoute StyleExceptionKind = "public-route"
)

// StyleException records one exact, auditable style-policy exception.
type StyleException struct {
	Kind     StyleExceptionKind
	Package  string
	Symbol   string
	Receiver string
	Method   string
	Type     string
	Reason   string
	Issue    string
}

// StyleExceptions is the sorted set of style-policy exceptions.
type StyleExceptions []StyleException

// AllowPackageFunction permits one exact package-level function.
func AllowPackageFunction(packagePath, symbol, reason, issue string) StyleException {
	return StyleException{
		Kind:    StylePackageFunction,
		Package: packagePath,
		Symbol:  symbol,
		Reason:  reason,
		Issue:   issue,
	}
}

// AllowPackageVariable permits one exact package-level variable of one exact
// Go type.
func AllowPackageVariable(packagePath, symbol, goType, reason, issue string) StyleException {
	return StyleException{
		Kind:    StylePackageVariable,
		Package: packagePath,
		Symbol:  symbol,
		Type:    goType,
		Reason:  reason,
		Issue:   issue,
	}
}

// AllowPublicRoute classifies one exact controller method as public.
func AllowPublicRoute(packagePath, receiver, method, reason, issue string) StyleException {
	return StyleException{
		Kind:     StylePublicRoute,
		Package:  packagePath,
		Receiver: receiver,
		Method:   method,
		Reason:   reason,
		Issue:    issue,
	}
}

// ViewOverrideKind identifies one exceptional View placement.
type ViewOverrideKind string

const (
	// ViewPlaceType places one exact canonical Go type in a View group.
	ViewPlaceType ViewOverrideKind = "place-type"
)

// ViewOverride records an exceptional mapping from canonical Go identity to a
// project-relative View group.
type ViewOverride struct {
	Kind      ViewOverrideKind
	GoSymbol  string
	ViewGroup string
}

// ViewOverrides is the sorted set of exceptional View mappings.
type ViewOverrides []ViewOverride

// PlaceType assigns one canonical Go type to an exceptional View group.
func PlaceType(goSymbol, viewGroup string) ViewOverride {
	return ViewOverride{
		Kind:      ViewPlaceType,
		GoSymbol:  goSymbol,
		ViewGroup: viewGroup,
	}
}

// PackageFormat identifies one requested distribution format.
type PackageFormat string

const (
	// PackageBinary emits a platform-native executable.
	PackageBinary PackageFormat = "binary"
	// PackageArchive emits an archive containing the application distribution.
	PackageArchive PackageFormat = "archive"
	// PackageModule emits a publishable ordinary Go module artifact.
	PackageModule PackageFormat = "module"
)

// Packaging declares deterministic distribution formats.
type Packaging struct {
	Formats []PackageFormat
}

// Catalog defines optional shared dependency versions and aliases.
type Catalog struct {
	Versions  Versions
	Libraries CatalogDependencies
	Starters  CatalogDependencies
}

// Versions maps stable catalog aliases to Go module versions.
type Versions map[string]string

// CatalogDependency names one approved module and either a literal version or
// a key in Catalog.Versions.
type CatalogDependency struct {
	Module  string
	Version string
}

// CatalogDependencies maps stable aliases to approved dependencies.
type CatalogDependencies map[string]CatalogDependency

// CatalogLibrary creates one cataloged ordinary Go module.
func CatalogLibrary(module, version string) CatalogDependency {
	return CatalogDependency{Module: module, Version: version}
}

// CatalogStarter creates one cataloged starter module.
func CatalogStarter(module, version string) CatalogDependency {
	return CatalogDependency{Module: module, Version: version}
}

// Local defines machine-local, non-secret project substitutions.
type Local struct {
	Replacements      Replacements
	ToolPaths         ToolPaths
	WorkspaceProvider WorkspaceProvider
}

// Replacement maps one Go module to a local checkout.
type Replacement struct {
	Module    string
	Directory string
}

// Replacements is the ordered set of local module replacements.
type Replacements []Replacement

// Replace declares one local Go module replacement.
func Replace(module, directory string) Replacement {
	return Replacement{Module: module, Directory: directory}
}

// ToolPaths maps stable tool names to machine-local executable paths.
type ToolPaths map[string]string

// WorkspaceProvider identifies a local workspace projection provider.
type WorkspaceProvider string

const (
	// MaterializedWorkspace selects the portable materialized projection.
	MaterializedWorkspace WorkspaceProvider = "materialized"
)
