package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	modulePath         = "github.com/spice-framework/starter-websocket"
	maxModuleGraphSize = 16 << 20
)

type listedModule struct {
	Path    string
	Version string
	Replace string
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string            `json:"name"`
	SPDXID           string            `json:"SPDXID"`
	VersionInfo      string            `json:"versionInfo"`
	DownloadLocation string            `json:"downloadLocation"`
	FilesAnalyzed    bool              `json:"filesAnalyzed"`
	LicenseConcluded string            `json:"licenseConcluded"`
	LicenseDeclared  string            `json:"licenseDeclared"`
	CopyrightText    string            `json:"copyrightText"`
	ExternalRefs     []spdxExternalRef `json:"externalRefs,omitempty"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func buildSBOM(ctx context.Context, config Config) ([]byte, error) {
	modules, err := committedModules(ctx, config)
	if err != nil {
		return nil, err
	}
	rootID := packageID(modulePath, config.Version)
	packages := []spdxPackage{newSPDXPackage(modulePath, config.Version, "")}
	relationships := []spdxRelationship{{
		SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: rootID,
	}}
	for _, module := range modules {
		item := newSPDXPackage(module.Path, module.Version, module.Replace)
		packages = append(packages, item)
		relationships = append(relationships, spdxRelationship{
			SPDXElementID: rootID, RelationshipType: "DEPENDS_ON", RelatedSPDXElement: item.SPDXID,
		})
	}
	var identity strings.Builder
	identity.WriteString(config.Version)
	identity.WriteByte('\n')
	identity.WriteString(config.Commit)
	for _, item := range packages {
		identity.WriteByte('\n')
		identity.WriteString(item.Name)
		identity.WriteByte('@')
		identity.WriteString(item.VersionInfo)
	}
	namespaceHash := sha256.Sum256([]byte(identity.String()))
	document := spdxDocument{
		SPDXVersion: "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        "Spice WebSocket starter " + config.Version,
		DocumentNamespace: "https://github.com/spice-framework/starter-websocket/releases/" +
			config.Version + "/spdx/" + hex.EncodeToString(namespaceHash[:]),
		CreationInfo: spdxCreationInfo{
			Created: config.Epoch.Format(time.RFC3339),
			Creators: []string{
				"Organization: Spice Authors",
				"Tool: github.com/spice-framework/starter-websocket/cmd/starter-websocket-release",
			},
		},
		Packages: packages, Relationships: relationships,
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode release SBOM: %w", err)
	}
	return append(data, '\n'), nil
}

func committedModules(ctx context.Context, config Config) ([]listedModule, error) {
	goMod, err := committedFile(ctx, config.Root, config.Commit, "go.mod")
	if err != nil {
		return nil, err
	}
	metadata, err := parseCommittedModfile(ctx, config.Root, goMod)
	if err != nil {
		return nil, err
	}
	if metadata.Module.Path != modulePath {
		return nil, fmt.Errorf("committed go.mod does not declare %s", modulePath)
	}
	selected, err := selectedModules(metadata)
	if err != nil {
		return nil, err
	}
	goSum, err := committedFile(ctx, config.Root, config.Commit, "go.sum")
	if err != nil {
		return nil, err
	}
	if sumErr := validateModuleSums(selected, goSum); sumErr != nil {
		return nil, sumErr
	}
	vendor, err := committedFile(ctx, config.Root, config.Commit, "vendor/modules.txt")
	if err != nil {
		return nil, err
	}
	if len(vendor) > maxModuleGraphSize {
		return nil, fmt.Errorf("committed vendor graph exceeds %d bytes", maxModuleGraphSize)
	}
	var result []listedModule
	for line := range strings.SplitSeq(string(vendor), "\n") {
		if !strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			continue
		}
		if module, ok := parseVendorModule(strings.TrimPrefix(line, "# ")); ok {
			if isLocalReplacement(module.Replace) {
				return nil, fmt.Errorf("committed vendor graph contains local replacement %q", module.Replace)
			}
			result = append(result, module)
		}
	}
	if err := validateVendorGraph(selected, result); err != nil {
		return nil, err
	}
	slices.SortFunc(result, func(left, right listedModule) int { return strings.Compare(left.Path, right.Path) })
	return result, nil
}

func parseVendorModule(line string) (listedModule, bool) {
	left, replacement, replaced := strings.Cut(line, " => ")
	fields := strings.Fields(left)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		return listedModule{}, false
	}
	module := listedModule{Path: fields[0], Version: fields[1]}
	if replaced {
		module.Replace = strings.TrimSpace(replacement)
	}
	return module, true
}

func isLocalReplacement(value string) bool {
	return strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") ||
		len(value) > 2 && value[1] == ':'
}

func newSPDXPackage(name, version, replacement string) spdxPackage {
	item := spdxPackage{
		Name: name, SPDXID: packageID(name, version), VersionInfo: version,
		DownloadLocation: "NOASSERTION", FilesAnalyzed: false,
		LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION",
	}
	if replacement != "" {
		item.ExternalRefs = []spdxExternalRef{{
			ReferenceCategory: "OTHER", ReferenceType: "spice:go-replace", ReferenceLocator: replacement,
		}}
	}
	return item
}

func packageID(name, version string) string {
	sum := sha256.Sum256([]byte(name + "@" + version))
	return "SPDXRef-Package-" + hex.EncodeToString(sum[:8])
}
