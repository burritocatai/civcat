package downloader

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func createTestZip(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(dir, "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestExtractZip(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"workflow1.json":        `{"nodes":[]}`,
		"subdir/workflow2.json": `{"nodes":[1]}`,
	}
	zipPath := createTestZip(t, tmpDir, files)

	destDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractZip(zipPath, destDir); err != nil {
		t.Fatalf("extractZip() error: %v", err)
	}

	for name, wantContent := range files {
		got, err := os.ReadFile(filepath.Join(destDir, name))
		if err != nil {
			t.Errorf("reading extracted file %s: %v", name, err)
			continue
		}
		if string(got) != wantContent {
			t.Errorf("file %s: got %q, want %q", name, got, wantContent)
		}
	}
}

func TestIsZipFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"workflow.zip", true},
		{"workflow.ZIP", true},
		{"workflow.Zip", true},
		{"model.safetensors", false},
		{"archive.tar.gz", false},
		{"noext", false},
	}
	for _, tt := range tests {
		if got := isZipFile(tt.name); got != tt.want {
			t.Errorf("isZipFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestExtractZipSlipProtection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a zip with a path traversal entry.
	zipPath := filepath.Join(tmpDir, "malicious.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create("../../../etc/evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("malicious"))
	w.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "output")
	os.MkdirAll(destDir, 0o755)

	if err := extractZip(zipPath, destDir); err != nil {
		t.Fatalf("extractZip() error: %v", err)
	}

	// The malicious file should not exist outside destDir.
	evilPath := filepath.Join(tmpDir, "etc", "evil.txt")
	if _, err := os.Stat(evilPath); !os.IsNotExist(err) {
		t.Error("zip slip protection failed: file was extracted outside target directory")
	}
}
