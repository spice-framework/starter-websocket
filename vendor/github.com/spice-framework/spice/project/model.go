package project

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/spice-framework/spice/project/schema"
)

// SourceSetID identifies one logical project source set.
type SourceSetID string

const (
	// SourceSetMain contains production Go and resource files.
	SourceSetMain SourceSetID = "main"
	// SourceSetTest contains test Go and resource files.
	SourceSetTest SourceSetID = "test"
	// SourceSetGenerated contains generated, read-only files.
	SourceSetGenerated SourceSetID = "generated"
)

// Role classifies a file or View group without changing its Go identity.
type Role string

const (
	// RoleProject identifies an application entrypoint or project-root file.
	RoleProject Role = "project"
	// RoleDomain identifies domain values and behavior.
	RoleDomain Role = "domain"
	// RoleApplication identifies application services and use cases.
	RoleApplication Role = "application"
	// RolePersistence identifies repository and persistence adapters.
	RolePersistence Role = "persistence"
	// RoleWeb identifies HTTP and other inbound web adapters.
	RoleWeb Role = "web"
	// RoleConfiguration identifies configuration declarations.
	RoleConfiguration Role = "configuration"
	// RoleEvents identifies event declarations and listeners.
	RoleEvents Role = "events"
	// RoleResource identifies non-Go resource files.
	RoleResource Role = "resource"
	// RoleGenerated identifies generated source and artifacts.
	RoleGenerated Role = "generated"
)

// ProjectIdentity is the path-independent identity of one Spice project.
type ProjectIdentity struct {
	Name   string `json:"name"`
	Module string `json:"module"`
	Kind   Kind   `json:"kind"`
}

// SourceSet describes the stable View roots for one logical source set.
type SourceSet struct {
	ID           SourceSetID `json:"id"`
	GoRoot       string      `json:"goRoot,omitempty"`
	ResourceRoot string      `json:"resourceRoot,omitempty"`
}

// PackageRecord describes one canonical Go package represented in the model.
type PackageRecord struct {
	GoPackagePath string `json:"goPackagePath"`
	GoPackageName string `json:"goPackageName"`
	Feature       string `json:"feature,omitempty"`
	Module        string `json:"module,omitempty"`
}

// ViewRecord describes one projected architectural group.
type ViewRecord struct {
	ID            string      `json:"id"`
	Path          string      `json:"path"`
	Feature       string      `json:"feature,omitempty"`
	Role          Role        `json:"role"`
	SourceSet     SourceSetID `json:"sourceSet"`
	GoPackagePath string      `json:"goPackagePath,omitempty"`
}

// FileRecord provides the reversible canonical/View mapping for one file.
type FileRecord struct {
	ID            string      `json:"id"`
	CanonicalPath string      `json:"canonicalPath"`
	ViewPath      string      `json:"viewPath"`
	GoPackagePath string      `json:"goPackagePath,omitempty"`
	GoPackageName string      `json:"goPackageName,omitempty"`
	SourceSet     SourceSetID `json:"sourceSet"`
	Role          Role        `json:"role"`
	PrimarySymbol string      `json:"primarySymbol,omitempty"`
	Generated     bool        `json:"generated"`
	ReadOnly      bool        `json:"readOnly"`
	ContentHash   string      `json:"contentHash"`
}

// ResolvedDependency records one dependency after Go module resolution.
type ResolvedDependency struct {
	ID           string          `json:"id"`
	Kind         DependencyKind  `json:"kind"`
	Scope        DependencyScope `json:"scope"`
	Name         string          `json:"name,omitempty"`
	Module       string          `json:"module"`
	Version      string          `json:"version"`
	Direct       bool            `json:"direct"`
	Capabilities []string        `json:"capabilities,omitempty"`
}

// TargetRecord describes one resolved application or library build target.
type TargetRecord struct {
	Name                   string `json:"name"`
	Kind                   Kind   `json:"kind"`
	GoPackagePath          string `json:"goPackagePath"`
	GeneratedGoPackagePath string `json:"generatedGoPackagePath,omitempty"`
}

// ProjectModel is the deterministic wire representation of the canonical
// Spice Project Model. Physical root directories are deliberately absent;
// CanonicalPath values are project-relative ordinary Go checkout paths.
type ProjectModel struct {
	Schema       string               `json:"schema"`
	Project      ProjectIdentity      `json:"project"`
	SourceSets   []SourceSet          `json:"sourceSets"`
	Packages     []PackageRecord      `json:"packages"`
	Views        []ViewRecord         `json:"views"`
	Files        []FileRecord         `json:"files"`
	Dependencies []ResolvedDependency `json:"dependencies"`
	Targets      []TargetRecord       `json:"targets"`
}

// AgentFileRecord is the agent-safe file model. It intentionally has no
// canonical physical path field.
type AgentFileRecord struct {
	ID            string      `json:"id"`
	ViewPath      string      `json:"viewPath"`
	GoPackagePath string      `json:"goPackagePath,omitempty"`
	GoPackageName string      `json:"goPackageName,omitempty"`
	SourceSet     SourceSetID `json:"sourceSet"`
	Role          Role        `json:"role"`
	PrimarySymbol string      `json:"primarySymbol,omitempty"`
	Generated     bool        `json:"generated"`
	ReadOnly      bool        `json:"readOnly"`
	ContentHash   string      `json:"contentHash"`
}

// AgentProjectModel is the canonical-path-free model exposed to coding
// agents by default.
type AgentProjectModel struct {
	Schema       string               `json:"schema"`
	Project      ProjectIdentity      `json:"project"`
	SourceSets   []SourceSet          `json:"sourceSets"`
	Packages     []PackageRecord      `json:"packages"`
	Views        []ViewRecord         `json:"views"`
	Files        []AgentFileRecord    `json:"files"`
	Dependencies []ResolvedDependency `json:"dependencies"`
	Targets      []TargetRecord       `json:"targets"`
}

// NewProjectModel validates and deterministically normalizes a complete
// Project Model.
func NewProjectModel(model ProjectModel) (ProjectModel, error) {
	normalized := cloneProjectModel(model)
	normalizeProjectModel(&normalized)
	if err := validateProjectModel(normalized); err != nil {
		return ProjectModel{}, err
	}
	return normalized, nil
}

// ParseProjectModel strictly decodes and validates a complete Project Model.
func ParseProjectModel(content []byte) (ProjectModel, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var model ProjectModel
	if err := decoder.Decode(&model); err != nil {
		return ProjectModel{}, fmt.Errorf("decode Spice Project Model: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ProjectModel{}, errors.New("decode Spice Project Model: trailing JSON value")
		}
		return ProjectModel{}, fmt.Errorf("decode Spice Project Model trailing data: %w", err)
	}
	normalized, err := NewProjectModel(model)
	if err != nil {
		return ProjectModel{}, fmt.Errorf("validate Spice Project Model: %w", err)
	}
	return normalized, nil
}

// JSON returns canonical indented Project Model JSON with a final newline.
func (model ProjectModel) JSON() ([]byte, error) {
	normalized, err := NewProjectModel(model)
	if err != nil {
		return nil, fmt.Errorf("encode Spice Project Model: %w", err)
	}
	return canonicalJSON(normalized)
}

// Agent returns the canonical-path-free model that tools expose to coding
// agents by default.
func (model ProjectModel) Agent() (AgentProjectModel, error) {
	normalized, err := NewProjectModel(model)
	if err != nil {
		return AgentProjectModel{}, fmt.Errorf("project agent model: %w", err)
	}
	files := make([]AgentFileRecord, len(normalized.Files))
	for index, file := range normalized.Files {
		files[index] = AgentFileRecord{
			ID:            file.ID,
			ViewPath:      file.ViewPath,
			GoPackagePath: file.GoPackagePath,
			GoPackageName: file.GoPackageName,
			SourceSet:     file.SourceSet,
			Role:          file.Role,
			PrimarySymbol: file.PrimarySymbol,
			Generated:     file.Generated,
			ReadOnly:      file.ReadOnly,
			ContentHash:   file.ContentHash,
		}
	}
	return AgentProjectModel{
		Schema:       schema.AgentProjectModel,
		Project:      normalized.Project,
		SourceSets:   slices.Clone(normalized.SourceSets),
		Packages:     slices.Clone(normalized.Packages),
		Views:        slices.Clone(normalized.Views),
		Files:        files,
		Dependencies: cloneResolvedDependencies(normalized.Dependencies),
		Targets:      slices.Clone(normalized.Targets),
	}, nil
}

// JSON returns canonical indented agent Project Model JSON with a final
// newline.
func (model AgentProjectModel) JSON() ([]byte, error) {
	normalized, err := NewAgentProjectModel(model)
	if err != nil {
		return nil, fmt.Errorf("encode agent Project Model: %w", err)
	}
	return canonicalJSON(normalized)
}

// NewAgentProjectModel validates and deterministically normalizes an
// agent-safe Project Model.
func NewAgentProjectModel(model AgentProjectModel) (AgentProjectModel, error) {
	normalized := cloneAgentProjectModel(model)
	normalizeAgentProjectModel(&normalized)
	if err := validateAgentProjectModel(normalized); err != nil {
		return AgentProjectModel{}, err
	}
	return normalized, nil
}

// ParseAgentProjectModel strictly decodes and validates an agent-safe Project
// Model.
func ParseAgentProjectModel(content []byte) (AgentProjectModel, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var model AgentProjectModel
	if err := decoder.Decode(&model); err != nil {
		return AgentProjectModel{}, fmt.Errorf("decode agent Spice Project Model: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return AgentProjectModel{}, errors.New("decode agent Spice Project Model: trailing JSON value")
		}
		return AgentProjectModel{}, fmt.Errorf("decode agent Spice Project Model trailing data: %w", err)
	}
	normalized, err := NewAgentProjectModel(model)
	if err != nil {
		return AgentProjectModel{}, fmt.Errorf("validate agent Spice Project Model: %w", err)
	}
	return normalized, nil
}

func validateProjectModel(model ProjectModel) error {
	if model.Schema != schema.ProjectModel {
		return fmt.Errorf("project model schema must be %q, got %q", schema.ProjectModel, model.Schema)
	}
	if err := validateProjectIdentity(model.Project); err != nil {
		return err
	}
	sourceSets, err := validateModelSourceSets(model.SourceSets)
	if err != nil {
		return err
	}
	packages, err := validateModelPackages(model.Project.Module, model.Packages)
	if err != nil {
		return err
	}
	if err := validateModelViews(model.Views, sourceSets, packages); err != nil {
		return err
	}
	if err := validateModelFiles(model.Files, sourceSets, packages); err != nil {
		return err
	}
	if err := validateResolvedDependencies(model.Dependencies); err != nil {
		return err
	}
	return validateModelTargets(model.Targets, packages, model.Project.Module)
}

func validateAgentProjectModel(model AgentProjectModel) error {
	if model.Schema != schema.AgentProjectModel {
		return fmt.Errorf("agent Project Model schema must be %q, got %q", schema.AgentProjectModel, model.Schema)
	}
	if err := validateProjectIdentity(model.Project); err != nil {
		return err
	}
	sourceSets, err := validateModelSourceSets(model.SourceSets)
	if err != nil {
		return err
	}
	packages, err := validateModelPackages(model.Project.Module, model.Packages)
	if err != nil {
		return err
	}
	if err := validateModelViews(model.Views, sourceSets, packages); err != nil {
		return err
	}
	if err := validateAgentFiles(model.Files, sourceSets, packages); err != nil {
		return err
	}
	if err := validateResolvedDependencies(model.Dependencies); err != nil {
		return err
	}
	return validateModelTargets(model.Targets, packages, model.Project.Module)
}

func validateProjectIdentity(identity ProjectIdentity) error {
	if !portableNamePattern.MatchString(identity.Name) {
		return fmt.Errorf("project model project name %q is invalid", identity.Name)
	}
	if !validModulePath(identity.Module) {
		return fmt.Errorf("project model module %q is invalid", identity.Module)
	}
	if !validKind(identity.Kind) {
		return fmt.Errorf("project model kind %q is unsupported", identity.Kind)
	}
	return nil
}

func validateModelSourceSets(sourceSets []SourceSet) (map[SourceSetID]struct{}, error) {
	seen := make(map[SourceSetID]struct{}, len(sourceSets))
	paths := make(map[string]struct{}, len(sourceSets)*2)
	for _, sourceSet := range sourceSets {
		if sourceSet.ID != SourceSetMain && sourceSet.ID != SourceSetTest && sourceSet.ID != SourceSetGenerated {
			return nil, fmt.Errorf("project model source set %q is unsupported", sourceSet.ID)
		}
		if sourceSet.GoRoot == "" && sourceSet.ResourceRoot == "" {
			return nil, fmt.Errorf("project model source set %q has no roots", sourceSet.ID)
		}
		if _, duplicate := seen[sourceSet.ID]; duplicate {
			return nil, fmt.Errorf("project model source set %q is duplicated", sourceSet.ID)
		}
		seen[sourceSet.ID] = struct{}{}
		for _, root := range []string{sourceSet.GoRoot, sourceSet.ResourceRoot} {
			if root == "" {
				continue
			}
			if !validRelativePath(root) {
				return nil, fmt.Errorf("project model source root %q is invalid", root)
			}
			folded := strings.ToLower(root)
			if _, duplicate := paths[folded]; duplicate {
				return nil, fmt.Errorf("project model source root %q collides after case folding", root)
			}
			paths[folded] = struct{}{}
		}
	}
	return seen, nil
}

func validateModelPackages(projectModule string, packages []PackageRecord) (map[string]string, error) {
	seen := make(map[string]string, len(packages))
	for _, packageRecord := range packages {
		if !validModulePath(packageRecord.GoPackagePath) ||
			(packageRecord.GoPackagePath != projectModule && !strings.HasPrefix(packageRecord.GoPackagePath, projectModule+"/")) {
			return nil, fmt.Errorf("project model Go package %q does not belong to module %q", packageRecord.GoPackagePath, projectModule)
		}
		if !validPackageName(packageRecord.GoPackageName) {
			return nil, fmt.Errorf("project model Go package name %q is invalid", packageRecord.GoPackageName)
		}
		if packageRecord.Feature != "" && !portableNamePattern.MatchString(packageRecord.Feature) {
			return nil, fmt.Errorf("project model feature %q is invalid", packageRecord.Feature)
		}
		if packageRecord.Module != "" && !validModulePath(packageRecord.Module) {
			return nil, fmt.Errorf("project model application module %q is invalid", packageRecord.Module)
		}
		if _, duplicate := seen[packageRecord.GoPackagePath]; duplicate {
			return nil, fmt.Errorf("project model Go package %q is duplicated", packageRecord.GoPackagePath)
		}
		seen[packageRecord.GoPackagePath] = packageRecord.GoPackageName
	}
	return seen, nil
}

func validateModelViews(views []ViewRecord, sourceSets map[SourceSetID]struct{}, packages map[string]string) error {
	seenIDs := make(map[string]struct{}, len(views))
	seenPaths := make(map[string]struct{}, len(views))
	for _, view := range views {
		if !validRelativePath(view.ID) || !validRelativePath(view.Path) {
			return fmt.Errorf("project model View %q has an invalid identity or path", view.ID)
		}
		if !validRole(view.Role) {
			return fmt.Errorf("project model View %q role %q is unsupported", view.ID, view.Role)
		}
		if _, exists := sourceSets[view.SourceSet]; !exists {
			return fmt.Errorf("project model View %q references unknown source set %q", view.ID, view.SourceSet)
		}
		if view.GoPackagePath != "" {
			if _, exists := packages[view.GoPackagePath]; !exists {
				return fmt.Errorf("project model View %q references unknown Go package %q", view.ID, view.GoPackagePath)
			}
		}
		if view.Feature != "" && !portableNamePattern.MatchString(view.Feature) {
			return fmt.Errorf("project model View %q feature %q is invalid", view.ID, view.Feature)
		}
		if _, duplicate := seenIDs[view.ID]; duplicate {
			return fmt.Errorf("project model View ID %q is duplicated", view.ID)
		}
		foldedPath := strings.ToLower(view.Path)
		if _, duplicate := seenPaths[foldedPath]; duplicate {
			return fmt.Errorf("project model View path %q collides after case folding", view.Path)
		}
		seenIDs[view.ID] = struct{}{}
		seenPaths[foldedPath] = struct{}{}
	}
	return nil
}

func validateModelFiles(files []FileRecord, sourceSets map[SourceSetID]struct{}, packages map[string]string) error {
	seenIDs := make(map[string]struct{}, len(files))
	canonicalPaths := make(map[string]struct{}, len(files))
	viewPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		if err := validateModelFile(file, sourceSets, packages); err != nil {
			return err
		}
		if _, duplicate := seenIDs[file.ID]; duplicate {
			return fmt.Errorf("project model file ID %q is duplicated", file.ID)
		}
		canonical := strings.ToLower(file.CanonicalPath)
		if _, duplicate := canonicalPaths[canonical]; duplicate {
			return fmt.Errorf("project model canonical path %q collides after case folding", file.CanonicalPath)
		}
		view := strings.ToLower(file.ViewPath)
		if _, duplicate := viewPaths[view]; duplicate {
			return fmt.Errorf("project model View path %q collides after case folding", file.ViewPath)
		}
		seenIDs[file.ID] = struct{}{}
		canonicalPaths[canonical] = struct{}{}
		viewPaths[view] = struct{}{}
	}
	return nil
}

func validateModelFile(file FileRecord, sourceSets map[SourceSetID]struct{}, packages map[string]string) error {
	if strings.TrimSpace(file.ID) == "" || strings.TrimSpace(file.ID) != file.ID {
		return errors.New("project model file requires a trimmed non-empty ID")
	}
	if !validRelativePath(file.CanonicalPath) || !validRelativePath(file.ViewPath) {
		return fmt.Errorf("project model file %q has an invalid canonical or View path", file.ID)
	}
	if _, exists := sourceSets[file.SourceSet]; !exists {
		return fmt.Errorf("project model file %q references unknown source set %q", file.ID, file.SourceSet)
	}
	if !validRole(file.Role) {
		return fmt.Errorf("project model file %q role %q is unsupported", file.ID, file.Role)
	}
	if err := validateFilePackage(file, packages); err != nil {
		return err
	}
	if file.PrimarySymbol != "" && !validGoIdentifier(file.PrimarySymbol) {
		return fmt.Errorf("project model file %q primary symbol %q is invalid", file.ID, file.PrimarySymbol)
	}
	if file.Generated && (!file.ReadOnly || file.SourceSet != SourceSetGenerated) {
		return fmt.Errorf("project model generated file %q must be read-only in the generated source set", file.ID)
	}
	if !validSHA256(file.ContentHash) {
		return fmt.Errorf("project model file %q content hash is not lowercase SHA-256", file.ID)
	}
	return nil
}

func validateAgentFiles(files []AgentFileRecord, sourceSets map[SourceSetID]struct{}, packages map[string]string) error {
	seenIDs := make(map[string]struct{}, len(files))
	viewPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		if err := validateAgentFile(file, sourceSets, packages); err != nil {
			return err
		}
		if _, duplicate := seenIDs[file.ID]; duplicate {
			return fmt.Errorf("agent Project Model file ID %q is duplicated", file.ID)
		}
		folded := strings.ToLower(file.ViewPath)
		if _, duplicate := viewPaths[folded]; duplicate {
			return fmt.Errorf("agent Project Model View path %q collides after case folding", file.ViewPath)
		}
		seenIDs[file.ID] = struct{}{}
		viewPaths[folded] = struct{}{}
	}
	return nil
}

func validateAgentFile(file AgentFileRecord, sourceSets map[SourceSetID]struct{}, packages map[string]string) error {
	if strings.TrimSpace(file.ID) == "" || strings.TrimSpace(file.ID) != file.ID {
		return errors.New("agent Project Model file requires a trimmed non-empty ID")
	}
	if !validRelativePath(file.ViewPath) {
		return fmt.Errorf("agent Project Model file %q has an invalid View path", file.ID)
	}
	if _, exists := sourceSets[file.SourceSet]; !exists {
		return fmt.Errorf("agent Project Model file %q references unknown source set %q", file.ID, file.SourceSet)
	}
	if !validRole(file.Role) {
		return fmt.Errorf("agent Project Model file %q role %q is unsupported", file.ID, file.Role)
	}
	if err := validateFilePackage(FileRecord{
		ID:            file.ID,
		GoPackagePath: file.GoPackagePath,
		GoPackageName: file.GoPackageName,
	}, packages); err != nil {
		return err
	}
	if file.PrimarySymbol != "" && !validGoIdentifier(file.PrimarySymbol) {
		return fmt.Errorf("agent Project Model file %q primary symbol %q is invalid", file.ID, file.PrimarySymbol)
	}
	if file.Generated && (!file.ReadOnly || file.SourceSet != SourceSetGenerated) {
		return fmt.Errorf("agent Project Model generated file %q must be read-only in the generated source set", file.ID)
	}
	if !validSHA256(file.ContentHash) {
		return fmt.Errorf("agent Project Model file %q content hash is not lowercase SHA-256", file.ID)
	}
	return nil
}

func validateFilePackage(file FileRecord, packages map[string]string) error {
	if file.GoPackagePath == "" && file.GoPackageName == "" {
		return nil
	}
	name, exists := packages[file.GoPackagePath]
	if !exists {
		return fmt.Errorf("project model file %q references unknown Go package %q", file.ID, file.GoPackagePath)
	}
	if file.GoPackageName != name {
		return fmt.Errorf("project model file %q package name %q does not match %q", file.ID, file.GoPackageName, name)
	}
	return nil
}

func validateResolvedDependencies(dependencies []ResolvedDependency) error {
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		if err := validateResolvedDependency(dependency); err != nil {
			return err
		}
		if _, duplicate := seen[dependency.ID]; duplicate {
			return fmt.Errorf("project model dependency ID %q is duplicated", dependency.ID)
		}
		seen[dependency.ID] = struct{}{}
	}
	return nil
}

func validateResolvedDependency(dependency ResolvedDependency) error {
	if strings.TrimSpace(dependency.ID) == "" || strings.TrimSpace(dependency.ID) != dependency.ID {
		return errors.New("project model dependency requires a trimmed non-empty ID")
	}
	if dependency.Kind != DependencyStarter && dependency.Kind != DependencyLibrary && dependency.Kind != DependencyTool {
		return fmt.Errorf("project model dependency %q kind %q is unsupported", dependency.ID, dependency.Kind)
	}
	if dependency.Scope != ScopeMain && dependency.Scope != ScopeTest && dependency.Scope != ScopeBuild {
		return fmt.Errorf("project model dependency %q scope %q is unsupported", dependency.ID, dependency.Scope)
	}
	if !validVersionedModule(dependency.Module, dependency.Version) {
		return fmt.Errorf("project model dependency %q has an invalid resolved module or version", dependency.ID)
	}
	if dependency.Kind == DependencyStarter && !portableNamePattern.MatchString(dependency.Name) {
		return fmt.Errorf("project model starter dependency %q has an invalid name", dependency.ID)
	}
	if dependency.Kind != DependencyStarter && dependency.Name != "" {
		return fmt.Errorf("project model non-starter dependency %q cannot declare a starter name", dependency.ID)
	}
	if err := validateMetadataIdentities("dependency capabilities", dependency.Capabilities, catalogKeyPattern.MatchString); err != nil {
		return fmt.Errorf("project model dependency %q: %w", dependency.ID, err)
	}
	return nil
}

func validateModelTargets(targets []TargetRecord, packages map[string]string, projectModule string) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if !portableNamePattern.MatchString(target.Name) || !validKind(target.Kind) {
			return fmt.Errorf("project model target %q has an invalid name or kind", target.Name)
		}
		if _, exists := packages[target.GoPackagePath]; !exists {
			return fmt.Errorf("project model target %q references unknown Go package %q", target.Name, target.GoPackagePath)
		}
		if target.GeneratedGoPackagePath != "" {
			if !validModulePath(target.GeneratedGoPackagePath) ||
				!strings.HasPrefix(target.GeneratedGoPackagePath, projectModule+"/") {
				return fmt.Errorf("project model target %q generated Go package is invalid", target.Name)
			}
		}
		if _, duplicate := seen[target.Name]; duplicate {
			return fmt.Errorf("project model target %q is duplicated", target.Name)
		}
		seen[target.Name] = struct{}{}
	}
	return nil
}

func validRole(role Role) bool {
	switch role {
	case RoleProject, RoleDomain, RoleApplication, RolePersistence, RoleWeb,
		RoleConfiguration, RoleEvents, RoleResource, RoleGenerated:
		return true
	default:
		return false
	}
}

func validPackageName(name string) bool {
	if !validGoIdentifier(name) {
		return false
	}
	for _, character := range name {
		if unicode.IsUpper(character) {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func cloneProjectModel(model ProjectModel) ProjectModel {
	model.SourceSets = slices.Clone(model.SourceSets)
	model.Packages = slices.Clone(model.Packages)
	model.Views = slices.Clone(model.Views)
	model.Files = slices.Clone(model.Files)
	model.Dependencies = cloneResolvedDependencies(model.Dependencies)
	model.Targets = slices.Clone(model.Targets)
	return model
}

func cloneAgentProjectModel(model AgentProjectModel) AgentProjectModel {
	model.SourceSets = slices.Clone(model.SourceSets)
	model.Packages = slices.Clone(model.Packages)
	model.Views = slices.Clone(model.Views)
	model.Files = slices.Clone(model.Files)
	model.Dependencies = cloneResolvedDependencies(model.Dependencies)
	model.Targets = slices.Clone(model.Targets)
	return model
}

func cloneResolvedDependencies(dependencies []ResolvedDependency) []ResolvedDependency {
	cloned := slices.Clone(dependencies)
	for index := range cloned {
		cloned[index].Capabilities = slices.Clone(cloned[index].Capabilities)
	}
	return cloned
}

func normalizeProjectModel(model *ProjectModel) {
	slices.SortFunc(model.SourceSets, func(left, right SourceSet) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(model.Packages, func(left, right PackageRecord) int {
		return strings.Compare(left.GoPackagePath, right.GoPackagePath)
	})
	slices.SortFunc(model.Views, func(left, right ViewRecord) int {
		if compared := strings.Compare(left.Path, right.Path); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	slices.SortFunc(model.Files, func(left, right FileRecord) int {
		if compared := strings.Compare(left.ViewPath, right.ViewPath); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	for index := range model.Dependencies {
		slices.Sort(model.Dependencies[index].Capabilities)
	}
	slices.SortFunc(model.Dependencies, func(left, right ResolvedDependency) int {
		return strings.Compare(left.ID, right.ID)
	})
	slices.SortFunc(model.Targets, func(left, right TargetRecord) int {
		return strings.Compare(left.Name, right.Name)
	})
}

func normalizeAgentProjectModel(model *AgentProjectModel) {
	slices.SortFunc(model.SourceSets, func(left, right SourceSet) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(model.Packages, func(left, right PackageRecord) int {
		return strings.Compare(left.GoPackagePath, right.GoPackagePath)
	})
	slices.SortFunc(model.Views, func(left, right ViewRecord) int {
		if compared := strings.Compare(left.Path, right.Path); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	slices.SortFunc(model.Files, func(left, right AgentFileRecord) int {
		if compared := strings.Compare(left.ViewPath, right.ViewPath); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	for index := range model.Dependencies {
		slices.Sort(model.Dependencies[index].Capabilities)
	}
	slices.SortFunc(model.Dependencies, func(left, right ResolvedDependency) int {
		return strings.Compare(left.ID, right.ID)
	})
	slices.SortFunc(model.Targets, func(left, right TargetRecord) int {
		return strings.Compare(left.Name, right.Name)
	})
}

func canonicalJSON(value any) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
