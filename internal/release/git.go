package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxGitDiagnosticBytes = 32 << 10

func gitBytes(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	// #nosec G204 -- executable is fixed, the commit is validated hexadecimal,
	// and every remaining argument is a repository-owned literal.
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		diagnostic := stderr.Bytes()
		if len(diagnostic) > maxGitDiagnosticBytes {
			diagnostic = diagnostic[len(diagnostic)-maxGitDiagnosticBytes:]
		}
		return nil, errors.Join(err, fmt.Errorf("git: %s", strings.TrimSpace(string(diagnostic))))
	}
	return stdout.Bytes(), nil
}

func gitText(ctx context.Context, root string, arguments ...string) (string, error) {
	data, err := gitBytes(ctx, root, arguments...)
	return string(data), err
}

func commitEpoch(ctx context.Context, root, commit string) (time.Time, error) {
	output, err := gitText(ctx, root, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return time.Time{}, fmt.Errorf("read source commit epoch: %w", err)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse source commit epoch: %w", err)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func committedFile(ctx context.Context, root, commit, name string) ([]byte, error) {
	data, err := gitBytes(ctx, root, "show", commit+":"+name)
	if err != nil {
		return nil, fmt.Errorf("read committed %s: %w", name, err)
	}
	return data, nil
}
