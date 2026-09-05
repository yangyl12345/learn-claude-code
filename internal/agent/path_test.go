package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePathRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SafePath(root, "../secret"); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := SafePath(root, "link/secret"); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
