package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}
func TestCopyTreeLinksAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "target")
	if err := os.Mkdir(src, 0750); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(src, "кириллица.txt"), "original")
	if err := os.Symlink("кириллица.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "кириллица.txt"))
	if err != nil || string(b) != "original" {
		t.Fatalf("copy: %q %v", b, err)
	}
	target, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || target != "кириллица.txt" {
		t.Fatalf("link: %s %v", target, err)
	}
	put(t, filepath.Join(dst, "кириллица.txt"), "keep")
	if err := Copy(src, dst); err == nil {
		t.Fatal("overwrote directory")
	}
	if err := Copy(filepath.Join(src, "кириллица.txt"), filepath.Join(dst, "кириллица.txt")); err == nil {
		t.Fatal("overwrote file")
	}
	b, _ = os.ReadFile(filepath.Join(dst, "кириллица.txt"))
	if string(b) != "keep" {
		t.Fatal("target damaged")
	}
}
func TestCopyRejectsDescendantThroughSymlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	if err := Copy(src, filepath.Join(link, "nested")); err == nil {
		t.Fatal("recursive self copy accepted")
	}
	if _, err := os.Stat(filepath.Join(src, "nested")); !os.IsNotExist(err) {
		t.Fatal("created descendant")
	}
}
func TestMoveExclusive(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	put(t, src, "source")
	put(t, dst, "existing")
	if err := Move(src, dst); err == nil {
		t.Fatal("move overwrote existing destination")
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "existing" {
		t.Fatal("target changed")
	}
	renamed := filepath.Join(root, "renamed")
	if err := Move(src, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
}
func TestSaveDetectsExternalReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	put(t, path, "old")
	info, _ := os.Stat(path)
	other := filepath.Join(root, "other")
	put(t, other, "new")
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, []byte("clobber"), info); err == nil {
		t.Fatal("external replacement overwritten")
	}
	info, _ = os.Stat(path)
	if err := Save(path, []byte("saved"), info); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "saved" {
		t.Fatal("save failed")
	}
}
