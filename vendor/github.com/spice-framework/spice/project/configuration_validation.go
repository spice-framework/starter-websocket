package project

import (
	"errors"
	"fmt"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	goVersionPattern = regexp.MustCompile(`^1\.[0-9]+\.[0-9]+$`)
	versionPattern   = regexp.MustCompile(
		`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`,
	)
	portableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	catalogKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
)

// ValidateSettings validates one statically decoded settings declaration.
func ValidateSettings(settings Settings) error {
	if !portableNamePattern.MatchString(settings.Name) {
		return fmt.Errorf("project name %q is not a portable lowercase identity", settings.Name)
	}
	if !validModulePath(settings.Module) {
		return fmt.Errorf("project %q module %q is not a valid Go module path", settings.Name, settings.Module)
	}
	if !goVersionPattern.MatchString(settings.Toolchain.Go) {
		return fmt.Errorf("project %q Go version %q must use major.minor.patch form", settings.Name, settings.Toolchain.Go)
	}
	if !versionPattern.MatchString(settings.Toolchain.Spice) {
		return fmt.Errorf("project %q Spice version %q is not a v-prefixed semantic version", settings.Name, settings.Toolchain.Spice)
	}
	if err := validateIncludedProjects(settings.Projects); err != nil {
		return fmt.Errorf("project %q included projects: %w", settings.Name, err)
	}
	if err := validateDependencyPolicy(settings.DependencyPolicy); err != nil {
		return fmt.Errorf("project %q dependency policy: %w", settings.Name, err)
	}
	return nil
}

// ValidateBuild validates one statically decoded build declaration.
func ValidateBuild(build Build) error {
	if !validKind(build.Kind) {
		return fmt.Errorf("build kind %q is unsupported", build.Kind)
	}
	if err := validateDependencies(build.Dependencies); err != nil {
		return err
	}
	if err := validatePlugins(build.Kind, build.Plugins); err != nil {
		return err
	}
	if err := validateTargets(build.Targets); err != nil {
		return err
	}
	if err := validateGenerators(build.Generators); err != nil {
		return err
	}
	if err := validateStyleExceptions(build.StyleExceptions); err != nil {
		return err
	}
	if err := validateViewOverrides(build.Views); err != nil {
		return err
	}
	return validatePackaging(build.Packaging)
}

// ValidateCatalog validates one optional shared dependency catalog.
func ValidateCatalog(catalog Catalog) error {
	for alias, version := range catalog.Versions {
		if !catalogKeyPattern.MatchString(alias) {
			return fmt.Errorf("catalog version alias %q is invalid", alias)
		}
		if !versionPattern.MatchString(version) {
			return fmt.Errorf("catalog version %q value %q is not a v-prefixed semantic version", alias, version)
		}
	}
	if err := validateCatalogDependencies("library", catalog.Libraries, catalog.Versions); err != nil {
		return err
	}
	return validateCatalogDependencies("starter", catalog.Starters, catalog.Versions)
}

// ValidateLocal validates one optional machine-local, non-secret declaration.
func ValidateLocal(local Local) error {
	seenModules := make(map[string]struct{}, len(local.Replacements))
	for _, replacement := range local.Replacements {
		if !validModulePath(replacement.Module) {
			return fmt.Errorf("local replacement module %q is invalid", replacement.Module)
		}
		if !validLocalPath(replacement.Directory) {
			return fmt.Errorf("local replacement %q directory must be a non-empty path without control characters", replacement.Module)
		}
		if _, duplicate := seenModules[replacement.Module]; duplicate {
			return fmt.Errorf("local replacement module %q is duplicated", replacement.Module)
		}
		seenModules[replacement.Module] = struct{}{}
	}
	for name, toolPath := range local.ToolPaths {
		if !catalogKeyPattern.MatchString(name) {
			return fmt.Errorf("local tool name %q is invalid", name)
		}
		if !validLocalPath(toolPath) {
			return fmt.Errorf("local tool %q path must be a non-empty path without control characters", name)
		}
	}
	if local.WorkspaceProvider != "" && local.WorkspaceProvider != MaterializedWorkspace {
		return fmt.Errorf("local workspace provider %q is unsupported", local.WorkspaceProvider)
	}
	return nil
}

func validateIncludedProjects(projects IncludedProjects) error {
	seenNames := make(map[string]struct{}, len(projects))
	seenDirectories := make(map[string]struct{}, len(projects))
	for _, included := range projects {
		if !portableNamePattern.MatchString(included.Name) {
			return fmt.Errorf("name %q is invalid", included.Name)
		}
		if !validRelativePath(included.Directory) {
			return fmt.Errorf("project %q directory %q is not a canonical relative path", included.Name, included.Directory)
		}
		if _, duplicate := seenNames[included.Name]; duplicate {
			return fmt.Errorf("name %q is duplicated", included.Name)
		}
		if _, duplicate := seenDirectories[strings.ToLower(included.Directory)]; duplicate {
			return fmt.Errorf("directory %q is duplicated after case folding", included.Directory)
		}
		seenNames[included.Name] = struct{}{}
		seenDirectories[strings.ToLower(included.Directory)] = struct{}{}
	}
	return nil
}

func validateDependencyPolicy(policy DependencyPolicy) error {
	if policy.Verification != "" && policy.Verification != Strict {
		return fmt.Errorf("verification mode %q is unsupported", policy.Verification)
	}
	for _, values := range []struct {
		label  string
		values []string
	}{
		{"approved registries", policy.ApprovedRegistries},
		{"approved proxies", policy.ApprovedProxies},
		{"allowed modules", policy.AllowedModules},
		{"denied modules", policy.DeniedModules},
	} {
		if err := validateSortedStrings(values.label, values.values); err != nil {
			return err
		}
	}
	for _, module := range append(slices.Clone(policy.AllowedModules), policy.DeniedModules...) {
		if !validModulePath(module) {
			return fmt.Errorf("policy module %q is invalid", module)
		}
	}
	allowed := make(map[string]struct{}, len(policy.AllowedModules))
	for _, module := range policy.AllowedModules {
		allowed[module] = struct{}{}
	}
	for _, module := range policy.DeniedModules {
		if _, conflict := allowed[module]; conflict {
			return fmt.Errorf("module %q is both allowed and denied", module)
		}
	}
	return nil
}

func validateDependencies(dependencies Dependencies) error {
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		if err := validateDependency(dependency); err != nil {
			return err
		}
		key := string(dependency.Scope) + "\x00" + string(dependency.Kind) + "\x00" + dependency.Name + "\x00" + dependency.Module
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("dependency %q is duplicated in %s scope", dependencyIdentity(dependency), dependency.Scope)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateDependency(dependency Dependency) error {
	switch dependency.Kind {
	case DependencyStarter:
		return validateStarterDependency(dependency)
	case DependencyLibrary:
		return validateLibraryDependency(dependency)
	case DependencyTool:
		return validateToolDependency(dependency)
	default:
		return fmt.Errorf("dependency kind %q is unsupported", dependency.Kind)
	}
}

func validateStarterDependency(dependency Dependency) error {
	if dependency.Scope != ScopeMain && dependency.Scope != ScopeTest {
		return fmt.Errorf("starter %q has unsupported scope %q", dependency.Name, dependency.Scope)
	}
	if !portableNamePattern.MatchString(dependency.Name) || dependency.Module != "" {
		return errors.New("starter dependency requires a portable name and no module path")
	}
	if dependency.Version != "" && !versionPattern.MatchString(dependency.Version) {
		return fmt.Errorf("starter %q version %q is invalid", dependency.Name, dependency.Version)
	}
	return nil
}

func validateLibraryDependency(dependency Dependency) error {
	if dependency.Scope != ScopeMain && dependency.Scope != ScopeTest {
		return fmt.Errorf("library %q has unsupported scope %q", dependency.Module, dependency.Scope)
	}
	if dependency.Name != "" || !validVersionedModule(dependency.Module, dependency.Version) {
		return errors.New("library dependency requires a valid module and v-prefixed semantic version")
	}
	return nil
}

func validateToolDependency(dependency Dependency) error {
	if dependency.Scope != ScopeBuild || dependency.Name != "" || !validVersionedModule(dependency.Module, dependency.Version) {
		return errors.New("tool dependency requires build scope, a valid module, and a v-prefixed semantic version")
	}
	return nil
}

func validatePlugins(buildKind Kind, plugins Plugins) error {
	applicationPlugins := 0
	seen := make(map[string]struct{}, len(plugins))
	for _, plugin := range plugins {
		key := string(plugin.Kind) + "\x00" + plugin.Module
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("plugin %q is duplicated", plugin.Kind)
		}
		seen[key] = struct{}{}
		switch plugin.Kind {
		case PluginApplication:
			applicationPlugins++
			if plugin.Module != "" || plugin.Version != "" {
				return fmt.Errorf("application plugin cannot declare a module or version")
			}
		case PluginCompiler:
			if !validVersionedModule(plugin.Module, plugin.Version) {
				return fmt.Errorf("compiler plugin requires a valid module and v-prefixed semantic version")
			}
		default:
			return fmt.Errorf("plugin kind %q is unsupported", plugin.Kind)
		}
	}
	if buildKind == Application && applicationPlugins != 1 {
		return fmt.Errorf("application build requires exactly one application plugin, got %d", applicationPlugins)
	}
	if buildKind != Application && applicationPlugins != 0 {
		return fmt.Errorf("%s build cannot select the application plugin", buildKind)
	}
	return nil
}

func validateTargets(targets Targets) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if !portableNamePattern.MatchString(target.Name) {
			return fmt.Errorf("target name %q is invalid", target.Name)
		}
		if !validModulePath(target.Package) || !validModulePath(target.Generated) {
			return fmt.Errorf("target %q requires valid canonical and generated Go package paths", target.Name)
		}
		if _, duplicate := seen[target.Name]; duplicate {
			return fmt.Errorf("target %q is duplicated", target.Name)
		}
		seen[target.Name] = struct{}{}
	}
	return nil
}

func validateGenerators(generators Generators) error {
	seen := make(map[string]struct{}, len(generators))
	for _, generator := range generators {
		if !validVersionedModule(generator.Module, generator.Version) {
			return fmt.Errorf("generator requires a valid module and v-prefixed semantic version")
		}
		if _, duplicate := seen[generator.Module]; duplicate {
			return fmt.Errorf("generator module %q is duplicated", generator.Module)
		}
		seen[generator.Module] = struct{}{}
	}
	return nil
}

func validateStyleExceptions(exceptions StyleExceptions) error {
	keys := make([]string, len(exceptions))
	for index, exception := range exceptions {
		if !validModulePath(exception.Package) || strings.TrimSpace(exception.Reason) == "" || strings.TrimSpace(exception.Issue) == "" {
			return fmt.Errorf("style exception %d requires a Go package, reason, and issue", index)
		}
		if err := validateStyleExceptionFields(exception, index); err != nil {
			return err
		}
		keys[index] = styleExceptionKey(exception)
	}
	return validateCanonicalKeys("style exceptions", keys)
}

func validateStyleExceptionFields(exception StyleException, index int) error {
	switch exception.Kind {
	case StylePackageFunction:
		return validatePackageFunctionException(exception, index)
	case StylePackageVariable:
		return validatePackageVariableException(exception, index)
	case StylePublicRoute:
		return validatePublicRouteException(exception, index)
	default:
		return fmt.Errorf("style exception kind %q is unsupported", exception.Kind)
	}
}

func validatePackageFunctionException(exception StyleException, index int) error {
	if !validGoIdentifier(exception.Symbol) || exception.Receiver != "" || exception.Method != "" || exception.Type != "" {
		return fmt.Errorf("package-function exception %d has invalid fields", index)
	}
	return nil
}

func validatePackageVariableException(exception StyleException, index int) error {
	if !validGoIdentifier(exception.Symbol) || strings.TrimSpace(exception.Type) == "" || exception.Receiver != "" || exception.Method != "" {
		return fmt.Errorf("package-variable exception %d has invalid fields", index)
	}
	return nil
}

func validatePublicRouteException(exception StyleException, index int) error {
	if !validExportedIdentifier(exception.Receiver) || !validExportedIdentifier(exception.Method) || exception.Symbol != "" || exception.Type != "" {
		return fmt.Errorf("public-route exception %d has invalid fields", index)
	}
	return nil
}

func validateViewOverrides(overrides ViewOverrides) error {
	keys := make([]string, len(overrides))
	for index, override := range overrides {
		if override.Kind != ViewPlaceType {
			return fmt.Errorf("view override kind %q is unsupported", override.Kind)
		}
		if !validCanonicalSymbol(override.GoSymbol) {
			return fmt.Errorf("view override symbol %q is not a canonical exported Go type", override.GoSymbol)
		}
		if !validRelativePath(override.ViewGroup) {
			return fmt.Errorf("view override group %q is not a canonical relative path", override.ViewGroup)
		}
		keys[index] = override.GoSymbol
	}
	return validateCanonicalKeys("View overrides", keys)
}

func validatePackaging(packaging Packaging) error {
	values := make([]string, len(packaging.Formats))
	for index, format := range packaging.Formats {
		switch format {
		case PackageBinary, PackageArchive, PackageModule:
		default:
			return fmt.Errorf("package format %q is unsupported", format)
		}
		values[index] = string(format)
	}
	return validateCanonicalKeys("package formats", values)
}

func validateCatalogDependencies(label string, dependencies CatalogDependencies, versions Versions) error {
	for alias, dependency := range dependencies {
		if !catalogKeyPattern.MatchString(alias) {
			return fmt.Errorf("catalog %s alias %q is invalid", label, alias)
		}
		if !validModulePath(dependency.Module) {
			return fmt.Errorf("catalog %s %q module %q is invalid", label, alias, dependency.Module)
		}
		if !versionPattern.MatchString(dependency.Version) {
			if _, exists := versions[dependency.Version]; !exists {
				return fmt.Errorf("catalog %s %q version %q is neither semantic metadata nor a version alias", label, alias, dependency.Version)
			}
		}
	}
	return nil
}

func validateSortedStrings(label string, values []string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%s contains an empty or untrimmed value", label)
		}
	}
	return validateCanonicalKeys(label, values)
}

func validateCanonicalKeys(label string, values []string) error {
	if !slices.IsSorted(values) {
		return fmt.Errorf("%s are not in canonical order", label)
	}
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return fmt.Errorf("%s contains duplicate %q", label, values[index])
		}
	}
	return nil
}

func validKind(kind Kind) bool {
	switch kind {
	case Application, LibraryKind, StarterKind, ToolKind:
		return true
	default:
		return false
	}
}

func validVersionedModule(module, version string) bool {
	return validModulePath(module) && versionPattern.MatchString(version)
}

func validModulePath(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n\t") {
		return false
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if isASCIIAlphaNumeric(character) || strings.ContainsRune(".-_~", character) {
				continue
			}
			return false
		}
	}
	return true
}

func validRelativePath(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	if strings.ContainsAny(value, "\\:\x00\r\n\t") || strings.HasPrefix(value, "/") ||
		path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		return false
	}
	for segment := range strings.SplitSeq(value, "/") {
		if !validPortablePathSegment(segment) {
			return false
		}
	}
	return true
}

func validLocalPath(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func validCanonicalSymbol(value string) bool {
	dot := strings.LastIndexByte(value, '.')
	return dot > strings.LastIndexByte(value, '/') && dot > 0 &&
		validModulePath(value[:dot]) && validExportedIdentifier(value[dot+1:])
}

func validExportedIdentifier(value string) bool {
	return validGoIdentifier(value) && unicode.IsUpper([]rune(value)[0])
}

func validGoIdentifier(value string) bool {
	if value == "" {
		return false
	}
	if token.Lookup(value).IsKeyword() {
		return false
	}
	for index, character := range value {
		if character == '_' || unicode.IsLetter(character) || (index > 0 && unicode.IsDigit(character)) {
			continue
		}
		return false
	}
	return true
}

func dependencyIdentity(dependency Dependency) string {
	if dependency.Name != "" {
		return dependency.Name
	}
	return dependency.Module
}

func styleExceptionKey(exception StyleException) string {
	return strings.Join([]string{
		string(exception.Kind),
		exception.Package,
		exception.Symbol,
		exception.Receiver,
		exception.Method,
	}, "\x00")
}

func validPortablePathSegment(segment string) bool {
	if segment == "" || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return false
	}
	base := segment
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToLower(base) {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return false
	}
	for _, character := range segment {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}
