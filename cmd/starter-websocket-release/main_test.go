package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const testEpoch int64 = 1_710_000_000

func TestRunProductionAndRehearsal(t *testing.T) {
	t.Parallel()
	root := newCommandRepository(t)
	gitCommand(t, root, "tag", "v1.0.0")
	keyPath := filepath.Join(t.TempDir(), "release.key")
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	writeCommandFile(t, keyPath, base64.StdEncoding.EncodeToString(seed))
	production := filepath.Join(t.TempDir(), "production")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"-root=" + root, "-output=" + production, "-version=v1.0.0", "-signing-key=" + keyPath,
		"-source-date-epoch=" + strconv.FormatInt(testEpoch, 10),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("production code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "created 5 artifact(s)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(production, "checksums.txt.sig")); err != nil {
		t.Fatalf("signed release: %v", err)
	}

	writeCommandFile(t, filepath.Join(root, "dirty.txt"), "dirty\n")
	rehearsal := filepath.Join(t.TempDir(), "rehearsal")
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"-root=" + root, "-output=" + rehearsal, "-version=v0.0.0-rehearsal", "-rehearsal",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rehearsal code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(rehearsal, "checksums.txt.sig")); !os.IsNotExist(err) {
		t.Fatalf("rehearsal signature error = %v", err)
	}
}

func TestRunRejectsUnsafeReleaseStates(t *testing.T) {
	t.Parallel()
	t.Run("missing required inputs", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		if code := run(context.Background(), nil, &bytes.Buffer{}, &stderr); code != 2 {
			t.Fatalf("code = %d", code)
		}
	})
	t.Run("invalid version", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"-version=--bad", "-rehearsal"}, &bytes.Buffer{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "canonical") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("rehearsal with key", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"-version=v1.0.0", "-rehearsal", "-signing-key=key"}, &bytes.Buffer{}, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "always unsigned") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("epoch mismatch", func(t *testing.T) {
		t.Parallel()
		root := newCommandRepository(t)
		var stderr bytes.Buffer
		code := run(context.Background(), []string{
			"-root=" + root, "-version=v1.0.0", "-rehearsal", "-source-date-epoch=1",
		}, &bytes.Buffer{}, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "does not match") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("missing tag", func(t *testing.T) {
		t.Parallel()
		root := newCommandRepository(t)
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"-root=" + root, "-version=v1.0.0"}, &bytes.Buffer{}, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "exact release tag") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("dirty checkout", func(t *testing.T) {
		t.Parallel()
		root := newCommandRepository(t)
		gitCommand(t, root, "tag", "v1.0.0")
		writeCommandFile(t, filepath.Join(root, "untracked.txt"), "dirty\n")
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"-root=" + root, "-version=v1.0.0"}, &bytes.Buffer{}, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "including untracked") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
	t.Run("missing signing key", func(t *testing.T) {
		t.Parallel()
		root := newCommandRepository(t)
		gitCommand(t, root, "tag", "v1.0.0")
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"-root=" + root, "-version=v1.0.0"}, &bytes.Buffer{}, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "signing-key is required") {
			t.Fatalf("code = %d, stderr = %q", code, stderr.String())
		}
	})
}

func newCommandRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeCommandFile(t, filepath.Join(root, "go.mod"), "module github.com/spice-framework/starter-websocket\n\ngo 1.26.0\n\nrequire example.com/a v1.0.0\n")
	writeCommandFile(t, filepath.Join(root, "go.sum"), "example.com/a v1.0.0 h1:test\n")
	writeCommandFile(t, filepath.Join(root, "vendor", "modules.txt"), "# example.com/a v1.0.0\n## explicit; go 1.26\nexample.com/a\n")
	writeCommandFile(t, filepath.Join(root, "LICENSE"), "license\n")
	writeCommandFile(t, filepath.Join(root, "README.md"), "readme\n")
	gitCommand(t, root, "init", "-q")
	gitCommand(t, root, "config", "user.name", "Spice Test")
	gitCommand(t, root, "config", "user.email", "test@spice.invalid")
	gitCommand(t, root, "add", ".")
	command := exec.Command("git", "commit", "-q", "-m", "fixture") // #nosec G204 -- fixed test command.
	command.Dir = root
	date := strconv.FormatInt(testEpoch, 10) + " +0000"
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	return root
}

func gitCommand(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...) // #nosec G204 -- test arguments are fixed.
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func writeCommandFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
