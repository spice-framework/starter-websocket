package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fixtureEpoch int64 = 1_710_000_000

func TestBuildIsDeterministicAndVerifiable(t *testing.T) {
	t.Parallel()
	root, commit := newReleaseRepository(t)
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	keyText := []byte(base64.StdEncoding.EncodeToString(seed))
	epoch := time.Unix(fixtureEpoch, 0).UTC()
	outputs := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	for _, output := range outputs {
		result, err := Build(context.Background(), Config{
			Root: root, OutputDir: output, Version: "v1.2.3-rc.1+build.7",
			Commit: commit, Epoch: epoch, PrivateKey: keyText,
		})
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		want := []string{
			"checksums.txt", "checksums.txt.pem", "checksums.txt.sig",
			"starter-websocket_1.2.3-rc.1+build.7_sbom.spdx.json",
			"starter-websocket_1.2.3-rc.1+build.7_source.tar.gz",
		}
		if !slices.Equal(result.Files, want) {
			t.Fatalf("files = %v, want %v", result.Files, want)
		}
	}
	for _, name := range artifactNames(t, outputs[0]) {
		left := readFile(t, filepath.Join(outputs[0], name))
		right := readFile(t, filepath.Join(outputs[1], name))
		if !slices.Equal(left, right) {
			t.Errorf("artifact %s differs across identical builds", name)
		}
		if strings.Contains(string(left), root) || strings.Contains(string(left), base64.StdEncoding.EncodeToString(seed)) ||
			bytes.Contains(left, seed) {
			t.Errorf("artifact %s exposes a root path or signing key", name)
		}
	}
	verifyChecksumsAndSignature(t, outputs[0])
	verifySourceArchive(t, filepath.Join(outputs[0], "starter-websocket_1.2.3-rc.1+build.7_source.tar.gz"), epoch)
	verifySBOM(t, filepath.Join(outputs[0], "starter-websocket_1.2.3-rc.1+build.7_sbom.spdx.json"), commit, epoch)
}

func TestBuildUsesCommittedSourceAndSupportsUnsignedRehearsal(t *testing.T) {
	t.Parallel()
	root, commit := newReleaseRepository(t)
	writeFile(t, filepath.Join(root, "tracked.txt"), "dirty\n")
	writeFile(t, filepath.Join(root, "untracked-secret.txt"), "must not ship\n")
	committedContent := git(t, root, "show", commit+":tracked.txt")
	output := filepath.Join(root, "unsigned")
	result, err := Build(context.Background(), Config{
		Root: root, OutputDir: output, Version: "v0.0.0-rehearsal",
		Commit: commit, Epoch: time.Unix(fixtureEpoch, 0).UTC(), AllowUnsigned: true,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if slices.Contains(result.Files, "checksums.txt.sig") || slices.Contains(result.Files, "checksums.txt.pem") {
		t.Fatalf("unsigned files = %v", result.Files)
	}
	archive := readArchive(t, filepath.Join(output, "starter-websocket_0.0.0-rehearsal_source.tar.gz"))
	if got := string(archive["starter-websocket_0.0.0-rehearsal/tracked.txt"]); got != committedContent {
		t.Fatalf("tracked content = %q, committed content = %q", got, committedContent)
	} else if got == string(readFile(t, filepath.Join(root, "tracked.txt"))) {
		t.Fatal("source archive used dirty working-tree bytes instead of committed bytes")
	}
	if _, found := archive["starter-websocket_0.0.0-rehearsal/untracked-secret.txt"]; found {
		t.Fatal("untracked file appeared in source artifact")
	}
}

func TestBuildRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	root, commit := newReleaseRepository(t)
	epoch := time.Unix(fixtureEpoch, 0).UTC()
	valid := Config{
		Root: root, OutputDir: filepath.Join(root, "release"), Version: "v1.0.0",
		Commit: commit, Epoch: epoch, AllowUnsigned: true,
	}
	//nolint:staticcheck // Exercise Build's explicit nil-context boundary.
	if _, err := Build(nil, valid); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(canceled, valid); err == nil {
		t.Fatal("canceled build unexpectedly succeeded")
	}
	tests := map[string]struct {
		mutate func(*Config)
		text   string
	}{
		"invalid version":    {func(config *Config) { config.Version = "1.0.0" }, "canonical"},
		"invalid commit":     {func(config *Config) { config.Commit = "HEAD" }, "full Git object ID"},
		"missing epoch":      {func(config *Config) { config.Epoch = time.Time{} }, "epoch is required"},
		"wrong epoch":        {func(config *Config) { config.Epoch = epoch.Add(time.Second) }, "does not match"},
		"signature required": {func(config *Config) { config.AllowUnsigned = false }, "signing key is required"},
		"key in rehearsal": {
			func(config *Config) { config.PrivateKey = bytes.Repeat([]byte{1}, ed25519.SeedSize) },
			"must not include",
		},
		"bad key": {func(config *Config) {
			config.AllowUnsigned = false
			config.PrivateKey = []byte("bad")
		}, "parse release signing key"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := valid
			config.OutputDir = filepath.Join(root, strings.ReplaceAll(name, " ", "-"))
			test.mutate(&config)
			_, err := Build(context.Background(), config)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Build() error = %v, want containing %q", err, test.text)
			}
		})
	}
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	valid.OutputDir = existing
	if _, err := Build(context.Background(), valid); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output error = %v", err)
	}
}

func TestBuildRejectsStaleCommittedGraphs(t *testing.T) {
	t.Parallel()
	t.Run("missing go.sum", func(t *testing.T) {
		t.Parallel()
		root, _ := newReleaseRepository(t)
		git(t, root, "rm", "go.sum")
		commit := commitFixture(t, root, "remove sum")
		_, err := Build(context.Background(), Config{
			Root: root, OutputDir: filepath.Join(root, "missing-sum"), Version: "v1.0.0",
			Commit: commit, Epoch: time.Unix(fixtureEpoch, 0).UTC(), AllowUnsigned: true,
		})
		if err == nil || !strings.Contains(err.Error(), "committed go.sum") {
			t.Fatalf("missing go.sum error = %v", err)
		}
	})
	t.Run("stale vendor", func(t *testing.T) {
		t.Parallel()
		root, _ := newReleaseRepository(t)
		vendor := readFile(t, filepath.Join(root, "vendor", "modules.txt"))
		stale := strings.Replace(string(vendor), "example.com/a v1.2.3", "example.com/a v1.2.4", 1)
		writeFile(t, filepath.Join(root, "vendor", "modules.txt"), stale)
		git(t, root, "add", "vendor/modules.txt")
		commit := commitFixture(t, root, "stale vendor")
		_, err := Build(context.Background(), Config{
			Root: root, OutputDir: filepath.Join(root, "stale-vendor"), Version: "v1.0.0",
			Commit: commit, Epoch: time.Unix(fixtureEpoch, 0).UTC(), AllowUnsigned: true,
		})
		if err == nil || !strings.Contains(err.Error(), "require v1.2.3") {
			t.Fatalf("stale vendor error = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(root, "stale-vendor")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed build committed output: %v", statErr)
		}
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".starter-websocket-release-") {
				t.Errorf("failed build retained staging directory %s", entry.Name())
			}
		}
	})
}

func TestValidationBoundaries(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v0.0.0", "v1.2.3-alpha.1", "v1.2.3+001"} {
		if err := ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q) = %v", version, err)
		}
	}
	for _, version := range []string{"", "v1", "v01.2.3", "v1.2.3-01", "v1.2.3+", "v1.2.3_4"} {
		if err := ValidateVersion(version); err == nil {
			t.Errorf("ValidateVersion(%q) unexpectedly succeeded", version)
		}
	}
	for _, test := range []struct{ name, target string }{
		{"empty", ""}, {"absolute", "/tmp/file"}, {"parent", "../file"}, {"windows", `dir\file`},
	} {
		if err := validateArchivePath(test.target); err == nil {
			t.Errorf("validateArchivePath(%s) unexpectedly succeeded", test.name)
		}
	}
	if err := validateSymlink("docs/link", "../../secret"); err == nil {
		t.Fatal("escaping symlink unexpectedly succeeded")
	}
	if _, err := parsePrivateKey([]byte("not a key")); err == nil {
		t.Fatal("invalid private key unexpectedly succeeded")
	}
	if module, ok := parseVendorModule("example.com/module v1.2.3 => example.com/fork v1.2.4"); !ok || module.Replace == "" {
		t.Fatalf("parseVendorModule() = %#v, %v", module, ok)
	}
	if err := validateVendorGraph(
		[]listedModule{{Path: "example.com/a", Version: "v1.0.0"}},
		[]listedModule{{Path: "example.com/a", Version: "v1.0.0"}, {Path: "example.com/a", Version: "v1.0.0"}},
	); err == nil {
		t.Fatal("duplicate vendor modules unexpectedly succeeded")
	}
	if err := validateModuleSums([]listedModule{{Path: "example.com/a", Version: "v1.0.0"}}, nil); err == nil {
		t.Fatal("missing module sum unexpectedly succeeded")
	}
	duplicateReplacement := modMetadata{}
	duplicateReplacement.Replace = append(
		duplicateReplacement.Replace,
		struct {
			Old modIdentity
			New modIdentity
		}{Old: modIdentity{Path: "example.com/a"}, New: modIdentity{Path: "example.com/b", Version: "v1.0.0"}},
		struct {
			Old modIdentity
			New modIdentity
		}{Old: modIdentity{Path: "example.com/a"}, New: modIdentity{Path: "example.com/c", Version: "v1.0.0"}},
	)
	if _, err := selectedModules(duplicateReplacement); err == nil {
		t.Fatal("duplicate replacement unexpectedly succeeded")
	}
	localReplacement := modMetadata{}
	localReplacement.Replace = append(localReplacement.Replace, struct {
		Old modIdentity
		New modIdentity
	}{Old: modIdentity{Path: "example.com/a"}, New: modIdentity{Path: "../local"}})
	if _, err := selectedModules(localReplacement); err == nil {
		t.Fatal("local replacement unexpectedly succeeded")
	}
}

func verifyChecksumsAndSignature(t *testing.T, root string) {
	t.Helper()
	checksums := readFile(t, filepath.Join(root, "checksums.txt"))
	for line := range strings.SplitSeq(strings.TrimSpace(string(checksums)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("invalid checksum line %q", line)
		}
		data := readFile(t, filepath.Join(root, fields[1]))
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != fields[0] {
			t.Fatalf("checksum mismatch for %s", fields[1])
		}
	}
	block, _ := pem.Decode(readFile(t, filepath.Join(root, "checksums.txt.pem")))
	if block == nil {
		t.Fatal("public key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok || !ed25519.Verify(publicKey, checksums, readFile(t, filepath.Join(root, "checksums.txt.sig"))) {
		t.Fatal("checksum signature did not verify")
	}
}

func verifySourceArchive(t *testing.T, name string, epoch time.Time) {
	t.Helper()
	file, err := os.Open(name) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close archive: %v", closeErr)
		}
	})
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	if !gzipReader.ModTime.Equal(epoch) {
		t.Fatalf("gzip epoch = %s", gzipReader.ModTime)
	}
	tarReader := tar.NewReader(gzipReader)
	found := false
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		found = true
		if !strings.HasPrefix(header.Name, "starter-websocket_1.2.3-rc.1+build.7/") || filepath.IsAbs(header.Name) {
			t.Errorf("unsafe archive path %q", header.Name)
		}
		if !header.ModTime.Equal(epoch) || !header.AccessTime.Equal(epoch) || !header.ChangeTime.Equal(epoch) {
			t.Errorf("entry %q has non-canonical timestamps", header.Name)
		}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Errorf("entry %q exposes ownership", header.Name)
		}
		if header.Typeflag == tar.TypeReg && header.Mode != 0o644 && header.Mode != 0o755 {
			t.Errorf("entry %q has non-canonical mode %o", header.Name, header.Mode)
		}
	}
	if !found {
		t.Fatal("source archive was empty")
	}
}

func verifySBOM(t *testing.T, name, commit string, epoch time.Time) {
	t.Helper()
	var document spdxDocument
	if err := json.Unmarshal(readFile(t, name), &document); err != nil {
		t.Fatal(err)
	}
	if document.SPDXVersion != "SPDX-2.3" || document.CreationInfo.Created != epoch.Format(time.RFC3339) {
		t.Fatalf("unexpected SBOM metadata: %#v", document.CreationInfo)
	}
	if len(document.Packages) != 3 || document.Packages[0].Name != modulePath {
		t.Fatalf("SBOM packages = %#v", document.Packages)
	}
	if !strings.Contains(document.DocumentNamespace, "/v1.2.3-rc.1+build.7/spdx/") {
		t.Fatalf("namespace = %q", document.DocumentNamespace)
	}
	if strings.Contains(string(readFile(t, name)), `:\`) || !strings.Contains(document.DocumentNamespace, "starter-websocket") {
		t.Fatalf("SBOM contains an absolute path or wrong namespace for %s", commit)
	}
}

func newReleaseRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module "+modulePath+"\n\ngo 1.26.0\n\nrequire (\n\texample.com/a v1.2.3\n\texample.com/b/v2 v2.0.0\n)\n")
	writeFile(t, filepath.Join(root, "go.sum"), "example.com/a v1.2.3 h1:test\nexample.com/b/v2 v2.0.0 h1:test\n")
	writeFile(t, filepath.Join(root, "vendor", "modules.txt"), "# example.com/a v1.2.3\n## explicit; go 1.26\nexample.com/a\n# example.com/b/v2 v2.0.0\n## explicit; go 1.26\nexample.com/b/v2\n")
	writeFile(t, filepath.Join(root, "LICENSE"), "license\n")
	writeFile(t, filepath.Join(root, "README.md"), "readme\n")
	writeFile(t, filepath.Join(root, "tracked.txt"), "committed\n")
	writeFile(t, filepath.Join(root, "script.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(root, "script.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Spice Test")
	git(t, root, "config", "user.email", "test@spice.invalid")
	git(t, root, "config", "core.autocrlf", "true")
	git(t, root, "add", ".")
	commitFixture(t, root, "fixture")
	return root, strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
}

func commitFixture(t *testing.T, root, message string) string {
	t.Helper()
	command := exec.Command("git", "commit", "-q", "-m", message) // #nosec G204 -- fixed test command.
	command.Dir = root
	date := strconv.FormatInt(fixtureEpoch, 10) + " +0000"
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	return strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
}

func git(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...) // #nosec G204 -- test arguments are fixed.
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func artifactNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	slices.Sort(result)
	return result
}

func readArchive(t *testing.T, name string) map[string][]byte {
	t.Helper()
	file, err := os.Open(name) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close archive: %v", closeErr)
		}
	})
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gzipReader)
	result := make(map[string][]byte)
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return result
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatal(readErr)
		}
		result[header.Name] = data
	}
}
