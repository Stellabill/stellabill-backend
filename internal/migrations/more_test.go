package migrations

import (
	"path/filepath"
	"testing"
)

func TestLoadDir_NoMigrations(t *testing.T) {
	_, err := LoadDir(t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadDir_ReadDirError(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadDir_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0000_bad.up.sql"), "SELECT 1;")
	writeFile(t, filepath.Join(dir, "0000_bad.down.sql"), "SELECT -1;")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadSQLFile_Missing(t *testing.T) {
	if _, err := readSQLFile(filepath.Join(t.TempDir(), "does-not-exist.sql")); err == nil {
		t.Fatalf("expected error")
	}
}

func TestFindByVersion_NotFound(t *testing.T) {
	if _, ok := FindByVersion([]Migration{{Version: 1, Name: "a"}}, 2); ok {
		t.Fatalf("expected not found")
	}
}
