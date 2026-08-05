// Command starter-websocket-release builds deterministic signed library artifacts.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	starterrelease "github.com/spice-framework/starter-websocket/internal/release"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) // Entrypoint exception: report command status.
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("starter-websocket-release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root, output, version, signingKey string
	var explicitEpoch int64
	var rehearsal bool
	flags.StringVar(&root, "root", ".", "repository root")
	flags.StringVar(&output, "output", "dist", "new release output directory")
	flags.StringVar(&version, "version", "", "canonical v-prefixed release version")
	flags.StringVar(&signingKey, "signing-key", "", "Ed25519 private-key file")
	flags.Int64Var(&explicitEpoch, "source-date-epoch", 0, "must equal the source commit epoch")
	flags.BoolVar(&rehearsal, "rehearsal", false, "explicitly build unsigned artifacts without tag/clean checks")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || version == "" {
		return writeExit(stderr, 2, "starter-websocket-release: -version is required and positional arguments are not accepted\n")
	}
	if err := starterrelease.ValidateVersion(version); err != nil {
		return writeExit(stderr, 2, "starter-websocket-release: %v\n", err)
	}
	if rehearsal && signingKey != "" {
		return writeExit(stderr, 2, "starter-websocket-release: rehearsal is always unsigned; remove -signing-key\n")
	}
	commit, epoch, err := sourceIdentity(ctx, root, explicitEpoch)
	if err != nil {
		return writeExit(stderr, 1, "starter-websocket-release: %v\n", err)
	}
	if !rehearsal {
		if validationErr := validateProduction(ctx, root, version, commit); validationErr != nil {
			return writeExit(stderr, 1, "starter-websocket-release: %v\n", validationErr)
		}
	}
	var key []byte
	if !rehearsal {
		if signingKey == "" {
			return writeExit(stderr, 1, "starter-websocket-release: -signing-key is required outside rehearsal\n")
		}
		key, err = readKey(signingKey)
		if err != nil {
			return writeExit(stderr, 1, "starter-websocket-release: read signing key: %v\n", err)
		}
	}
	result, err := starterrelease.Build(ctx, starterrelease.Config{
		Root: root, OutputDir: output, Version: version, Commit: commit, Epoch: epoch,
		PrivateKey: key, AllowUnsigned: rehearsal,
	})
	if err != nil {
		return writeExit(stderr, 1, "starter-websocket-release: %v\n", err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"starter-websocket %s: created %d artifact(s) in %s\n",
		version,
		len(result.Files),
		result.OutputDir,
	); err != nil {
		return 1
	}
	return 0
}

func sourceIdentity(ctx context.Context, root string, explicitEpoch int64) (string, time.Time, error) {
	commit, err := gitOutput(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("resolve HEAD commit: %w", err)
	}
	commit = strings.TrimSpace(commit)
	value, err := gitOutput(ctx, root, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read HEAD epoch: %w", err)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse HEAD epoch: %w", err)
	}
	if explicitEpoch != 0 && explicitEpoch != seconds {
		return "", time.Time{}, fmt.Errorf("source-date-epoch %d does not match HEAD epoch %d", explicitEpoch, seconds)
	}
	return commit, time.Unix(seconds, 0).UTC(), nil
}

func validateProduction(ctx context.Context, root, version, commit string) error {
	status, err := gitOutput(ctx, root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect release checkout: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("release checkout must be clean, including untracked files")
	}
	tagCommit, err := gitOutput(ctx, root, "rev-list", "-n", "1", version)
	if err != nil {
		return fmt.Errorf("resolve exact release tag %q: %w", version, err)
	}
	if strings.TrimSpace(tagCommit) != commit {
		return fmt.Errorf("release tag %q does not resolve to HEAD", version)
	}
	tags, err := gitOutput(ctx, root, "tag", "--points-at", commit)
	if err != nil {
		return fmt.Errorf("inspect release tags: %w", err)
	}
	if !slices.Contains(strings.Fields(tags), version) {
		return fmt.Errorf("release HEAD is not tagged exactly %q", version)
	}
	return nil
}

func gitOutput(ctx context.Context, root string, arguments ...string) (string, error) {
	// #nosec G204 -- Git is fixed and arguments are flags or canonical release inputs; no shell is used.
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", errors.Join(err, fmt.Errorf("git: %s", strings.TrimSpace(stderr.String())))
	}
	return stdout.String(), nil
}

func readKey(name string) ([]byte, error) {
	const maximum = 1 << 20
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	file, err := root.Open(filepath.Base(name))
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	if err := errors.Join(readErr, file.Close(), root.Close()); err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, fmt.Errorf("signing key exceeds %d bytes", maximum)
	}
	return data, nil
}

func writeExit(writer io.Writer, code int, format string, values ...any) int {
	if _, err := fmt.Fprintf(writer, format, values...); err != nil {
		return 1
	}
	return code
}
