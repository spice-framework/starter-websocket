// Package release builds deterministic, signed source releases from exact Git commits.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Config describes one exact-commit library release.
type Config struct {
	Root          string
	OutputDir     string
	Version       string
	Commit        string
	Epoch         time.Time
	PrivateKey    []byte
	AllowUnsigned bool
}

// Result describes the atomically committed artifact directory.
type Result struct {
	OutputDir string
	Files     []string
}

// Build creates a deterministic release directory without consulting the network.
func Build(ctx context.Context, config Config) (result Result, resultErr error) {
	normalized, err := normalizeConfig(ctx, config)
	if err != nil {
		return Result{}, err
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(normalized.OutputDir), 0o750); mkdirErr != nil {
		return Result{}, fmt.Errorf("create release parent: %w", mkdirErr)
	}
	staging, err := os.MkdirTemp(filepath.Dir(normalized.OutputDir), ".starter-websocket-release-*")
	if err != nil {
		return Result{}, fmt.Errorf("create release staging directory: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, os.RemoveAll(staging))
		}
	}()
	files, err := buildArtifacts(ctx, normalized, staging)
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(staging, normalized.OutputDir); err != nil {
		return Result{}, fmt.Errorf("commit release directory: %w", err)
	}
	committed = true
	return Result{OutputDir: normalized.OutputDir, Files: files}, nil
}

func normalizeConfig(ctx context.Context, config Config) (Config, error) {
	if inputErr := validateConfigInput(ctx, config); inputErr != nil {
		return Config{}, inputErr
	}
	root, output, pathErr := normalizePaths(config.Root, config.OutputDir)
	if pathErr != nil {
		return Config{}, pathErr
	}
	config.Root, config.OutputDir = root, output
	config.Epoch = config.Epoch.UTC().Truncate(time.Second)
	if commitErr := validateCommitSource(ctx, config); commitErr != nil {
		return Config{}, commitErr
	}
	config.PrivateKey = append([]byte(nil), config.PrivateKey...)
	return config, nil
}

func validateConfigInput(ctx context.Context, config Config) error {
	if ctx == nil {
		return errors.New("build release: context is nil")
	}
	if versionErr := ValidateVersion(config.Version); versionErr != nil {
		return fmt.Errorf("build release: %w", versionErr)
	}
	if !commitPattern.MatchString(config.Commit) {
		return fmt.Errorf("build release: commit %q is not a full Git object ID", config.Commit)
	}
	if config.Epoch.IsZero() {
		return errors.New("build release: source commit epoch is required")
	}
	if len(config.PrivateKey) == 0 && !config.AllowUnsigned {
		return errors.New("build release: Ed25519 signing key is required unless rehearsal is explicit")
	}
	if len(config.PrivateKey) != 0 && config.AllowUnsigned {
		return errors.New("build release: unsigned rehearsal must not include a signing key")
	}
	if len(config.PrivateKey) != 0 {
		if _, keyErr := parsePrivateKey(config.PrivateKey); keyErr != nil {
			return keyErr
		}
	}
	return nil
}

func normalizePaths(configuredRoot, configuredOutput string) (string, string, error) {
	root, err := filepath.Abs(configuredRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository root: %w", err)
	}
	output, err := filepath.Abs(configuredOutput)
	if err != nil {
		return "", "", fmt.Errorf("resolve output directory: %w", err)
	}
	if _, statErr := os.Stat(output); statErr == nil {
		return "", "", fmt.Errorf("build release: output directory %q already exists", output)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect output directory: %w", statErr)
	}
	return root, output, nil
}

func validateCommitSource(ctx context.Context, config Config) error {
	actualEpoch, err := commitEpoch(ctx, config.Root, config.Commit)
	if err != nil {
		return err
	}
	if !config.Epoch.Equal(actualEpoch) {
		return fmt.Errorf(
			"build release: epoch %d does not match source commit epoch %d",
			config.Epoch.Unix(), actualEpoch.Unix(),
		)
	}
	for _, name := range []string{"go.mod", "go.sum", "vendor/modules.txt", "LICENSE", "README.md"} {
		if _, fileErr := committedFile(ctx, config.Root, config.Commit, name); fileErr != nil {
			return fileErr
		}
	}
	return nil
}

func buildArtifacts(ctx context.Context, config Config, staging string) ([]string, error) {
	versionName := strings.TrimPrefix(config.Version, "v")
	archiveName := "starter-websocket_" + versionName + "_source.tar.gz"
	archive, err := buildSourceArchive(ctx, config)
	if err != nil {
		return nil, err
	}
	if writeErr := writeNew(filepath.Join(staging, archiveName), archive); writeErr != nil {
		return nil, writeErr
	}
	sbomName := "starter-websocket_" + versionName + "_sbom.spdx.json"
	sbom, err := buildSBOM(ctx, config)
	if err != nil {
		return nil, err
	}
	if writeErr := writeNew(filepath.Join(staging, sbomName), sbom); writeErr != nil {
		return nil, writeErr
	}
	files := []string{archiveName, sbomName}
	slices.Sort(files)
	checksums, err := checksums(staging, files)
	if err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(staging, "checksums.txt"), checksums); err != nil {
		return nil, err
	}
	files = append(files, "checksums.txt")
	if len(config.PrivateKey) != 0 {
		signature, publicKey, err := signChecksums(checksums, config.PrivateKey)
		if err != nil {
			return nil, err
		}
		for name, data := range map[string][]byte{
			"checksums.txt.pem": publicKey,
			"checksums.txt.sig": signature,
		} {
			if err := writeNew(filepath.Join(staging, name), data); err != nil {
				return nil, err
			}
			files = append(files, name)
		}
	}
	slices.Sort(files)
	return files, nil
}

func checksums(root string, names []string) ([]byte, error) {
	var output strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- name is generated internally.
		if err != nil {
			return nil, fmt.Errorf("read artifact %q: %w", name, err)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&output, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(output.String()), nil
}

func writeNew(name string, data []byte) error {
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return fmt.Errorf("open release artifact root: %w", err)
	}
	file, err := root.OpenFile(filepath.Base(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.Join(fmt.Errorf("create release artifact %q: %w", name, err), root.Close())
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close(), root.Close()); err != nil {
		return fmt.Errorf("write release artifact %q: %w", name, err)
	}
	return nil
}
