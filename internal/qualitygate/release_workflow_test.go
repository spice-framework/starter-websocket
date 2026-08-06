package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReleaseWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(string) string
		wantErr bool
	}{
		{name: "exact contract", mutate: func(content string) string { return content }},
		{name: "wrong revision", mutate: func(content string) string {
			return strings.Replace(content, releaseWorkflowRevision, strings.Repeat("0", 40), 1)
		}, wantErr: true},
		{name: "wrong module", mutate: func(content string) string { return strings.Replace(content, modulePath, modulePath+"-wrong", 1) }, wantErr: true},
		{name: "broad top-level permissions", mutate: func(content string) string {
			return strings.Replace(content, "permissions: {}", "permissions:\n  contents: write", 1)
		}, wantErr: true},
		{name: "missing job-local publish permission", mutate: func(content string) string {
			return strings.Replace(content, "    permissions:\n      contents: write\n", "", 1)
		}, wantErr: true},
		{name: "missing named signing secret", mutate: func(content string) string {
			return strings.Replace(content, "    secrets:\n      SPICE_LIBRARY_RELEASE_SIGNING_KEY: ${{ secrets.SPICE_LIBRARY_RELEASE_SIGNING_KEY }}\n", "", 1)
		}, wantErr: true},
		{name: "inherited unrelated secrets", mutate: func(content string) string {
			return strings.Replace(content, "    secrets:\n      SPICE_LIBRARY_RELEASE_SIGNING_KEY: ${{ secrets.SPICE_LIBRARY_RELEASE_SIGNING_KEY }}", "    secrets: inherit", 1)
		}, wantErr: true},
		{name: "wrong signing secret", mutate: func(content string) string {
			return strings.Replace(content, "secrets.SPICE_LIBRARY_RELEASE_SIGNING_KEY", "secrets.WRONG", 1)
		}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, ".github", "workflows", "release.yml")
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.mutate(expectedReleaseWorkflow(modulePath))), 0o600); err != nil {
				t.Fatal(err)
			}
			err := checkReleaseWorkflow(root)
			if (err != nil) != test.wantErr {
				t.Fatalf("checkReleaseWorkflow() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
