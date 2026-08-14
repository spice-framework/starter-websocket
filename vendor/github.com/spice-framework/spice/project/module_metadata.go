package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/spice-framework/spice/project/schema"
)

// SpiceCompatibility declares the supported Spice version interval for a
// published module. Current is optional compatibility evidence, not a second
// dependency resolver.
type SpiceCompatibility struct {
	Minimum string `json:"minimum"`
	Current string `json:"current,omitempty"`
}

// ModuleKind identifies a published Spice-native module artifact.
type ModuleKind string

const (
	// ModuleApplication identifies a published application distribution.
	ModuleApplication ModuleKind = "application"
	// ModuleLibrary identifies a reusable Spice-aware Go library.
	ModuleLibrary ModuleKind = "library"
	// ModuleStarter identifies an opt-in Spice starter module.
	ModuleStarter ModuleKind = "starter"
	// ModulePlugin identifies a Spice build or compiler plugin module.
	ModulePlugin ModuleKind = "plugin"
	// ModuleTool identifies a standalone Spice-aware tool module.
	ModuleTool ModuleKind = "tool"
)

// ModuleMetadata is the portable spice.module.json contract embedded in an
// ordinary authenticated Go module artifact.
type ModuleMetadata struct {
	Schema                    int                `json:"schema"`
	Kind                      ModuleKind         `json:"kind"`
	Name                      string             `json:"name"`
	Module                    string             `json:"module"`
	SpiceCompatibility        SpiceCompatibility `json:"spiceCompatibility"`
	Capabilities              []string           `json:"capabilities,omitempty"`
	Starters                  []string           `json:"starters,omitempty"`
	AnnotationPackages        []string           `json:"annotationPackages,omitempty"`
	ConfigurationPrefixes     []string           `json:"configurationPrefixes,omitempty"`
	CompilerTools             []string           `json:"compilerTools,omitempty"`
	PublicPackages            []string           `json:"publicPackages,omitempty"`
	Documentation             []string           `json:"documentation,omitempty"`
	GeneratedCodeRequirements []string           `json:"generatedCodeRequirements,omitempty"`
}

// NewModuleMetadata validates and deterministically normalizes module
// metadata.
func NewModuleMetadata(metadata ModuleMetadata) (ModuleMetadata, error) {
	normalized := cloneModuleMetadata(metadata)
	normalizeModuleMetadata(&normalized)
	if err := validateModuleMetadata(normalized); err != nil {
		return ModuleMetadata{}, err
	}
	return normalized, nil
}

// ParseModuleMetadata strictly decodes and validates spice.module.json bytes.
func ParseModuleMetadata(content []byte) (ModuleMetadata, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var metadata ModuleMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return ModuleMetadata{}, fmt.Errorf("decode Spice module metadata: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ModuleMetadata{}, errors.New("decode Spice module metadata: trailing JSON value")
		}
		return ModuleMetadata{}, fmt.Errorf("decode Spice module metadata trailing data: %w", err)
	}
	normalized, err := NewModuleMetadata(metadata)
	if err != nil {
		return ModuleMetadata{}, fmt.Errorf("validate Spice module metadata: %w", err)
	}
	return normalized, nil
}

// JSON returns canonical indented spice.module.json bytes with a final
// newline.
func (metadata ModuleMetadata) JSON() ([]byte, error) {
	normalized, err := NewModuleMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode Spice module metadata: %w", err)
	}
	content, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Spice module metadata: %w", err)
	}
	return append(content, '\n'), nil
}

func validateModuleMetadata(metadata ModuleMetadata) error {
	if metadata.Schema != schema.ModuleMetadata {
		return fmt.Errorf("spice module metadata schema must be %d, got %d", schema.ModuleMetadata, metadata.Schema)
	}
	if !validModuleKind(metadata.Kind) {
		return fmt.Errorf("spice module kind %q is unsupported", metadata.Kind)
	}
	if !portableNamePattern.MatchString(metadata.Name) {
		return fmt.Errorf("spice module name %q is not a portable lowercase identity", metadata.Name)
	}
	if !validModulePath(metadata.Module) {
		return fmt.Errorf("spice module path %q is invalid", metadata.Module)
	}
	if !versionPattern.MatchString(metadata.SpiceCompatibility.Minimum) {
		return fmt.Errorf("spice compatibility minimum %q is invalid", metadata.SpiceCompatibility.Minimum)
	}
	if metadata.SpiceCompatibility.Current != "" && !versionPattern.MatchString(metadata.SpiceCompatibility.Current) {
		return fmt.Errorf("spice compatibility current %q is invalid", metadata.SpiceCompatibility.Current)
	}
	if err := validateMetadataIdentities("capabilities", metadata.Capabilities, catalogKeyPattern.MatchString); err != nil {
		return err
	}
	if err := validateMetadataIdentities("starters", metadata.Starters, portableNamePattern.MatchString); err != nil {
		return err
	}
	if err := validateOwnedPackages("annotation packages", metadata.Module, metadata.AnnotationPackages); err != nil {
		return err
	}
	if err := validateMetadataIdentities("configuration prefixes", metadata.ConfigurationPrefixes, validConfigurationPrefix); err != nil {
		return err
	}
	if err := validateMetadataIdentities("compiler tools", metadata.CompilerTools, validModulePath); err != nil {
		return err
	}
	if err := validateOwnedPackages("public packages", metadata.Module, metadata.PublicPackages); err != nil {
		return err
	}
	if err := validateMetadataIdentities("documentation", metadata.Documentation, validDocumentationReference); err != nil {
		return err
	}
	return validateMetadataIdentities(
		"generated-code requirements",
		metadata.GeneratedCodeRequirements,
		catalogKeyPattern.MatchString,
	)
}

func validModuleKind(kind ModuleKind) bool {
	switch kind {
	case ModuleApplication, ModuleLibrary, ModuleStarter, ModulePlugin, ModuleTool:
		return true
	default:
		return false
	}
}

func validateOwnedPackages(label, module string, packages []string) error {
	return validateMetadataIdentities(label, packages, func(packagePath string) bool {
		return validModulePath(packagePath) &&
			(packagePath == module || strings.HasPrefix(packagePath, module+"/"))
	})
}

func validateMetadataIdentities(label string, values []string, valid func(string) bool) error {
	for _, value := range values {
		if !valid(value) {
			return fmt.Errorf("spice module metadata %s contains invalid value %q", label, value)
		}
	}
	return validateCanonicalKeys("spice module metadata "+label, values)
}

func validConfigurationPrefix(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for segment := range strings.SplitSeq(value, ".") {
		if !portableNamePattern.MatchString(segment) {
			return false
		}
	}
	return true
}

func validDocumentationReference(value string) bool {
	if strings.TrimSpace(value) != value || value == "" || strings.ContainsAny(value, "\x00\r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.IsAbs() {
		return parsed.Scheme == "https" && parsed.Host != ""
	}
	return validRelativePath(value)
}

func cloneModuleMetadata(metadata ModuleMetadata) ModuleMetadata {
	metadata.Capabilities = slices.Clone(metadata.Capabilities)
	metadata.Starters = slices.Clone(metadata.Starters)
	metadata.AnnotationPackages = slices.Clone(metadata.AnnotationPackages)
	metadata.ConfigurationPrefixes = slices.Clone(metadata.ConfigurationPrefixes)
	metadata.CompilerTools = slices.Clone(metadata.CompilerTools)
	metadata.PublicPackages = slices.Clone(metadata.PublicPackages)
	metadata.Documentation = slices.Clone(metadata.Documentation)
	metadata.GeneratedCodeRequirements = slices.Clone(metadata.GeneratedCodeRequirements)
	return metadata
}

func normalizeModuleMetadata(metadata *ModuleMetadata) {
	slices.Sort(metadata.Capabilities)
	slices.Sort(metadata.Starters)
	slices.Sort(metadata.AnnotationPackages)
	slices.Sort(metadata.ConfigurationPrefixes)
	slices.Sort(metadata.CompilerTools)
	slices.Sort(metadata.PublicPackages)
	slices.Sort(metadata.Documentation)
	slices.Sort(metadata.GeneratedCodeRequirements)
}
