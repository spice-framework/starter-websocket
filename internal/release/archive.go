package release

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	maxSourceTreeBytes = 16 << 20
	maxSourceBlobBytes = 128 << 20
)

type gitTreeEntry struct {
	mode string
	hash string
	name string
}

type sourceEntry struct {
	header tar.Header
	data   []byte
}

func buildSourceArchive(ctx context.Context, config Config) ([]byte, error) {
	tree, err := gitBytes(ctx, config.Root, "ls-tree", "-rz", "--full-tree", config.Commit)
	if err != nil {
		return nil, fmt.Errorf("list source commit tree: %w", err)
	}
	treeEntries, err := parseGitTree(tree)
	if err != nil {
		return nil, err
	}
	blobs, err := gitBlobs(ctx, config.Root, treeEntries)
	if err != nil {
		return nil, err
	}
	prefix := "starter-websocket_" + strings.TrimPrefix(config.Version, "v") + "/"
	entries := make([]sourceEntry, 0, len(treeEntries))
	for index, treeEntry := range treeEntries {
		entry, entryErr := makeSourceEntry(prefix, config.Epoch, treeEntry, blobs[index])
		if entryErr != nil {
			return nil, entryErr
		}
		entries = append(entries, entry)
	}
	return writeSourceArchive(entries, config.Epoch)
}

func parseGitTree(tree []byte) ([]gitTreeEntry, error) {
	if len(tree) > maxSourceTreeBytes {
		return nil, fmt.Errorf("source commit tree exceeds %d bytes", maxSourceTreeBytes)
	}
	records := bytes.Split(bytes.TrimSuffix(tree, []byte{0}), []byte{0})
	if len(records) == 1 && len(records[0]) == 0 {
		return nil, errors.New("source commit contains no tracked files")
	}
	entries := make([]gitTreeEntry, 0, len(records))
	for _, record := range records {
		metadata, name, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		if !found || len(fields) != 3 || fields[1] != "blob" {
			return nil, fmt.Errorf("source commit contains unsupported tree entry %q", record)
		}
		entries = append(entries, gitTreeEntry{mode: fields[0], hash: fields[2], name: string(name)})
	}
	slices.SortFunc(entries, func(left, right gitTreeEntry) int { return strings.Compare(left.name, right.name) })
	return entries, nil
}

func gitBlobs(ctx context.Context, root string, entries []gitTreeEntry) ([][]byte, error) {
	var input strings.Builder
	for _, entry := range entries {
		input.WriteString(entry.hash)
		input.WriteByte('\n')
	}
	// #nosec G204 -- executable, arguments, and object IDs from ls-tree are fixed or validated; no shell is used.
	command := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	command.Stdin = strings.NewReader(input.String())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("read committed source blobs: %w: %s", err, boundedGitText(stderr.Bytes()))
	}
	return parseGitBlobs(stdout.Bytes(), entries)
}

func parseGitBlobs(output []byte, entries []gitTreeEntry) ([][]byte, error) {
	reader := bufio.NewReader(bytes.NewReader(output))
	result := make([][]byte, 0, len(entries))
	for _, entry := range entries {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read committed source %q header: %w", entry.name, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != entry.hash || fields[1] != "blob" {
			return nil, fmt.Errorf("read committed source %q: invalid object header %q", entry.name, strings.TrimSpace(header))
		}
		size, parseErr := strconv.ParseInt(fields[2], 10, 64)
		if parseErr != nil || size < 0 || size > maxSourceBlobBytes {
			return nil, fmt.Errorf("read committed source %q: invalid blob size %q", entry.name, fields[2])
		}
		data := make([]byte, size)
		if _, readErr := io.ReadFull(reader, data); readErr != nil {
			return nil, fmt.Errorf("read committed source %q content: %w", entry.name, readErr)
		}
		terminator, readErr := reader.ReadByte()
		if readErr != nil || terminator != '\n' {
			return nil, fmt.Errorf("read committed source %q: invalid object terminator", entry.name)
		}
		result = append(result, data)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		return nil, errors.New("read committed source blobs: unexpected trailing output")
	}
	return result, nil
}

func makeSourceEntry(prefix string, epoch time.Time, treeEntry gitTreeEntry, data []byte) (sourceEntry, error) {
	if pathErr := validateArchivePath(treeEntry.name); pathErr != nil {
		return sourceEntry{}, pathErr
	}
	header := tar.Header{
		Name: prefix + treeEntry.name, ModTime: epoch, AccessTime: epoch, ChangeTime: epoch, Format: tar.FormatPAX,
	}
	entry := sourceEntry{header: header, data: data}
	switch treeEntry.mode {
	case "100644":
		entry.header.Mode = 0o644
		entry.header.Typeflag = tar.TypeReg
		entry.header.Size = int64(len(data))
	case "100755":
		entry.header.Mode = 0o755
		entry.header.Typeflag = tar.TypeReg
		entry.header.Size = int64(len(data))
	case "120000":
		entry.header.Mode = 0o777
		entry.header.Typeflag = tar.TypeSymlink
		entry.header.Linkname = string(data)
		entry.data = nil
		if linkErr := validateSymlink(treeEntry.name, entry.header.Linkname); linkErr != nil {
			return sourceEntry{}, linkErr
		}
	default:
		return sourceEntry{}, fmt.Errorf("source commit uses unsupported Git mode %q for %q", treeEntry.mode, treeEntry.name)
	}
	return entry, nil
}

func validateArchivePath(name string) error {
	clean := path.Clean(name)
	if name == "" || clean == "." || clean == ".." || clean != name || strings.HasPrefix(clean, "../") ||
		path.IsAbs(clean) || strings.Contains(name, "\\") {
		return fmt.Errorf("source commit contains unsafe path %q", name)
	}
	return nil
}

func validateSymlink(name, target string) error {
	if target == "" || path.IsAbs(target) || strings.Contains(target, "\\") {
		return fmt.Errorf("source symlink %q has unsafe target %q", name, target)
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("source symlink %q escapes archive root", name)
	}
	return nil
}

func writeSourceArchive(entries []sourceEntry, epoch time.Time) ([]byte, error) {
	var output bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("construct source gzip writer: %w", err)
	}
	gzipWriter.ModTime = epoch
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := tarWriter.WriteHeader(&entry.header); err != nil {
			return nil, errors.Join(err, tarWriter.Close(), gzipWriter.Close())
		}
		if _, err := tarWriter.Write(entry.data); err != nil {
			return nil, errors.Join(err, tarWriter.Close(), gzipWriter.Close())
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, errors.Join(err, gzipWriter.Close())
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close source gzip writer: %w", err)
	}
	return output.Bytes(), nil
}

func boundedGitText(data []byte) string {
	if len(data) > maxGitDiagnosticBytes {
		data = data[len(data)-maxGitDiagnosticBytes:]
	}
	return strings.TrimSpace(string(data))
}
