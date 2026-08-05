package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type modIdentity struct {
	Path    string
	Version string
}

type modMetadata struct {
	Module  modIdentity
	Require []struct {
		Path     string
		Version  string
		Indirect bool
	}
	Replace []struct {
		Old modIdentity
		New modIdentity
	}
}

func parseCommittedModfile(
	ctx context.Context,
	root string,
	content []byte,
) (metadata modMetadata, resultErr error) {
	file, err := os.CreateTemp("", "starter-websocket-release-*.mod")
	if err != nil {
		return modMetadata{}, fmt.Errorf("create committed modfile parser input: %w", err)
	}
	name := file.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove committed modfile parser input: %w", removeErr))
		}
	}()
	if _, err := file.Write(content); err != nil {
		return modMetadata{}, errors.Join(fmt.Errorf("write committed modfile parser input: %w", err), file.Close())
	}
	if err := file.Close(); err != nil {
		return modMetadata{}, fmt.Errorf("close committed modfile parser input: %w", err)
	}
	// #nosec G204 -- the command and flags are fixed; the modfile path comes from os.CreateTemp.
	command := exec.CommandContext(ctx, "go", "mod", "edit", "-json", "-modfile="+name)
	command.Dir = root
	command.Env = offlineGoEnvironment()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return modMetadata{}, fmt.Errorf("parse committed go.mod: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(stdout.Bytes(), &metadata); err != nil {
		return modMetadata{}, fmt.Errorf("decode committed go.mod: %w", err)
	}
	return metadata, nil
}

func offlineGoEnvironment() []string {
	blocked := map[string]struct{}{
		"GOFLAGS": {}, "GOPROXY": {}, "GOTOOLCHAIN": {}, "GOWORK": {},
	}
	result := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found {
			if _, skip := blocked[strings.ToUpper(name)]; !skip {
				result = append(result, entry)
			}
		}
	}
	return append(result, "GOFLAGS=", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off")
}

func selectedModules(metadata modMetadata) ([]listedModule, error) {
	replacements := make(map[string]modIdentity, len(metadata.Replace))
	for _, replacement := range metadata.Replace {
		key := replacement.Old.Path + "@" + replacement.Old.Version
		if _, duplicate := replacements[key]; duplicate {
			return nil, fmt.Errorf("committed go.mod has duplicate replacement for %s", key)
		}
		if replacement.New.Version == "" || isLocalReplacement(replacement.New.Path) {
			return nil, fmt.Errorf("committed go.mod contains local replacement %q", replacement.New.Path)
		}
		replacements[key] = replacement.New
	}
	seen := make(map[string]struct{}, len(metadata.Require))
	result := make([]listedModule, 0, len(metadata.Require))
	for _, requirement := range metadata.Require {
		if _, duplicate := seen[requirement.Path]; duplicate {
			return nil, fmt.Errorf("committed go.mod requires %s more than once", requirement.Path)
		}
		seen[requirement.Path] = struct{}{}
		module := listedModule{Path: requirement.Path, Version: requirement.Version}
		replacement, found := replacements[requirement.Path+"@"+requirement.Version]
		if !found {
			replacement, found = replacements[requirement.Path+"@"]
		}
		if found {
			module.Replace = replacement.Path + " " + replacement.Version
		}
		result = append(result, module)
	}
	return result, nil
}

func validateModuleSums(modules []listedModule, sums []byte) error {
	for _, module := range modules {
		path, version := module.Path, module.Version
		if module.Replace != "" {
			fields := strings.Fields(module.Replace)
			path, version = fields[0], fields[1]
		}
		if !containsModuleSum(sums, path, version) {
			return fmt.Errorf("committed go.sum has no checksum for %s %s", path, version)
		}
	}
	return nil
}

func containsModuleSum(content []byte, modulePath, version string) bool {
	for line := range strings.SplitSeq(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == modulePath &&
			(fields[1] == version || fields[1] == version+"/go.mod") {
			return true
		}
	}
	return false
}

func validateVendorGraph(expected, actual []listedModule) error {
	selected := make(map[string]listedModule, len(expected))
	for _, module := range expected {
		selected[module.Path] = module
	}
	seen := make(map[string]struct{}, len(actual))
	for _, module := range actual {
		if _, duplicate := seen[module.Path]; duplicate {
			return fmt.Errorf("committed vendor graph contains duplicate module %s", module.Path)
		}
		seen[module.Path] = struct{}{}
		wanted, found := selected[module.Path]
		if !found {
			return fmt.Errorf("committed vendor graph contains unselected module %s", module.Path)
		}
		if wanted.Version != module.Version || wanted.Replace != module.Replace {
			return fmt.Errorf("committed vendor module %s is %s => %s; require %s => %s",
				module.Path, module.Version, module.Replace, wanted.Version, wanted.Replace)
		}
	}
	if len(seen) != len(selected) {
		for path := range selected {
			if _, found := seen[path]; !found {
				return fmt.Errorf("committed vendor graph is missing selected module %s", path)
			}
		}
	}
	return nil
}
