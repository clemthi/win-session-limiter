package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyBinaryCopiesIntoNewDirUnderNewName(t *testing.T) {
	src := filepath.Join(t.TempDir(), "limiterd.exe")
	if err := os.WriteFile(src, []byte("binary-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "WinSessionL", ExeName)

	if err := copyBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "binary-v1" {
		t.Fatalf("copied content = %q, want %q", got, "binary-v1")
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file was left behind")
	}
}

func TestCopyBinaryOverwritesOlderCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), "limiterd.exe")
	if err := os.WriteFile(src, []byte("binary-v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), ExeName)
	if err := os.WriteFile(dst, []byte("binary-v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "binary-v2" {
		t.Fatalf("copied content = %q, want %q", got, "binary-v2")
	}
}

func TestCopyBinaryOntoItselfKeepsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ExeName)
	if err := os.WriteFile(path, []byte("binary-v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyBinary(path, path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "binary-v1" {
		t.Fatalf("content after self-copy = %q, want it unchanged", got)
	}
}

func TestCopyBinaryMissingSource(t *testing.T) {
	dst := filepath.Join(t.TempDir(), ExeName)
	if err := copyBinary(filepath.Join(t.TempDir(), "nope.exe"), dst); err == nil {
		t.Fatal("expected an error for a missing source binary")
	}
}
