package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderWritableUsesRealWriteAndCleansUp(t *testing.T) {
	dir := t.TempDir()

	if err := TestFolderWritable(dir); err != nil {
		t.Fatalf("writable directory rejected: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".weed-write-test-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("write probe files left behind: %v", matches)
	}
}

func TestFolderWritableRejectsMissingAndNonDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := TestFolderWritable(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}

	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TestFolderWritable(file); err == nil {
		t.Fatal("regular file accepted as writable directory")
	}
}

func TestFolderWritableRejectsEffectivePermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permission bits")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	if err := TestFolderWritable(dir); err == nil {
		t.Fatal("directory without effective write access accepted")
	}
}

func TestToShortFileName(t *testing.T) {
	tests := []struct {
		in    string
		value string
	}{
		{"/data/a/b/c/d.txt", "/data/a/b/c/d.txt"},
		{"/data/a/b/c/очень_длинное_имя_файла_c_подробным_указанием_наименования_и_содержания_стандартизованных_форм_за_анварь_-_июнь_2023_года(РОГА_И_КОПЫТА_ООО).txt", "/data/a/b/c/очень_длинное_имя_файла_c_подробным_указанием_наименования_и_содержания_стандартизованных_форм_за_анварь_-_июнь_2023_года(РОГА_И_КОПЫТ354fcaf4.txt"},
		{"/data/a/b/c/очень_длинное_имя_файла_c_подробным_указанием_наименования_и_содержания_стандартизованных_форм_за_анварь_-_июнь_2023_года(РОГА_И_КОПЫТА_ООО)_without_extension", "/data/a/b/c/очень_длинное_имя_файла_c_подробным_указанием_наименования_и_содержания_стандартизованных_форм_за_анварь_-_июнь_2023_года(РОГА_И_КОПЫТА_О21a6e47a"},
	}
	for _, p := range tests {
		got := ToShortFileName(p.in)
		if got != p.value {
			t.Errorf("failed to test: got %v, want %v", got, p.value)
		}
	}
}
