package migrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDir_LoadsAndSorts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0002_second.up.sql"), "SELECT 2;")
	writeFile(t, filepath.Join(dir, "0002_second.down.sql"), "SELECT -2;")
	writeFile(t, filepath.Join(dir, "0001_first.up.sql"), "SELECT 1;")
	writeFile(t, filepath.Join(dir, "0001_first.down.sql"), "SELECT -1;")

	migs, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(migs) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migs))
	}
	if migs[0].Version != 1 || migs[0].Name != "first" {
		t.Fatalf("unexpected first migration: %#v", migs[0])
	}
	if migs[1].Version != 2 || migs[1].Name != "second" {
		t.Fatalf("unexpected second migration: %#v", migs[1])
	}
}

func TestLoadDir_RequiresPairs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0001_first.up.sql"), "SELECT 1;")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadDir_MissingUp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0001_first.down.sql"), "SELECT -1;")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadDir_RejectsConflicts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0001_first.up.sql"), "SELECT 1;")
	writeFile(t, filepath.Join(dir, "0001_first.down.sql"), "SELECT -1;")
	writeFile(t, filepath.Join(dir, "0001_other.up.sql"), "SELECT 1;")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadDir_RejectsEmptySQL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0001_first.up.sql"), "   \n")
	writeFile(t, filepath.Join(dir, "0001_first.down.sql"), "SELECT -1;")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestFindByVersion(t *testing.T) {
	migs := []Migration{{Version: 1, Name: "a"}, {Version: 2, Name: "b"}}
	m, ok := FindByVersion(migs, 2)
	if !ok || m.Name != "b" {
		t.Fatalf("unexpected result: ok=%v m=%#v", ok, m)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestValidateSequence(t *testing.T) {
	t.Run("valid sequence", func(t *testing.T) {
		migs := []Migration{
			{Version: 1, Name: "a"},
			{Version: 2, Name: "b"},
			{Version: 3, Name: "c"},
		}
		if err := ValidateSequence(migs); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("missing version", func(t *testing.T) {
		migs := []Migration{
			{Version: 1, Name: "a"},
			{Version: 3, Name: "c"},
		}
		err := ValidateSequence(migs)
		if err == nil {
			t.Error("expected error, got nil")
		} else if err.Error() != "migration sequence error: expected version 2, got 3 for migration 'c'" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("starts not from 1", func(t *testing.T) {
		migs := []Migration{
			{Version: 2, Name: "b"},
			{Version: 3, Name: "c"},
		}
		err := ValidateSequence(migs)
		if err == nil {
			t.Error("expected error, got nil")
		} else if err.Error() != "migration sequence error: expected version 1, got 2 for migration 'b'" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
