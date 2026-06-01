package migrations

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRedactDatabaseURL(t *testing.T) {
	got := RedactDatabaseURL("postgres://user:pass@localhost:5432/db?sslmode=disable")
	if got == "postgres://user:pass@localhost:5432/db?sslmode=disable" {
		t.Fatalf("expected password to be redacted, got %q", got)
	}
	if got != "postgres://user@localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected redacted url: %q", got)
	}
}

func TestRedactDatabaseURL_Invalid(t *testing.T) {
	if got := RedactDatabaseURL("%%%"); got != "<invalid database url>" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_NoUserInfo(t *testing.T) {
	got := RedactDatabaseURL("postgres://localhost:5432/db?sslmode=disable")
	if got != "postgres://localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_UserWithoutPassword(t *testing.T) {
	got := RedactDatabaseURL("postgres://user@localhost:5432/db?sslmode=disable")
	if got != "postgres://user@localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRedactDatabaseURL_EmptyUsername(t *testing.T) {
	got := RedactDatabaseURL("postgres://:pass@localhost:5432/db?sslmode=disable")
	if got != "postgres://localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestParseMigrationFilename(t *testing.T) {
	tests := []struct {
		filename    string
		wantVersion int64
		wantName    string
		wantKind    string
		wantErr     bool
	}{
		{"0001_init.up.sql", 1, "init", "up", false},
		{"0002_create-table.down.sql", 2, "create-table", "down", false},
		{"9999_some_long_name_123.up.sql", 9999, "some_long_name_123", "up", false},
		{"001_init.up.sql", 0, "", "", true},       // 3 digits
		{"00001_init.up.sql", 0, "", "", true},     // 5 digits
		{"0001_init.sql", 0, "", "", true},         // missing up/down
		{"0001_init.up.txt", 0, "", "", true},      // wrong extension
		{"abcd_init.up.sql", 0, "", "", true},      // non-numeric version
		{"-0001_init.up.sql", 0, "", "", true},     // negative version sign
		{"0001_.up.sql", 0, "", "", true},          // empty name
		{"0001_init.up.sql.bak", 0, "", "", true},  // extra suffix
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			v, name, kind, err := ParseMigrationFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}
			if !tt.wantErr {
				if v != tt.wantVersion || name != tt.wantName || kind != tt.wantKind {
					t.Errorf("got (%d, %q, %q), want (%d, %q, %q)", v, name, kind, tt.wantVersion, tt.wantName, tt.wantKind)
				}
			}
		})
	}
}

func TestValidateFS(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid migrations sequence",
			files: map[string]string{
				"0001_init.up.sql":        "CREATE TABLE t1 (id INT);",
				"0001_init.down.sql":      "DROP TABLE t1;",
				"0002_add_field.up.sql":   "ALTER TABLE t1 ADD COLUMN name TEXT;",
				"0002_add_field.down.sql": "ALTER TABLE t1 DROP COLUMN name;",
				"migrations.go":           "package migrations", // ignored
			},
			wantErr: false,
		},
		{
			name: "missing down migration",
			files: map[string]string{
				"0001_init.up.sql":   "SELECT 1;",
				"0002_second.up.sql": "SELECT 2;",
				"0002_second.down.sql": "SELECT 2;",
			},
			wantErr: true,
			errMsg:  "missing down migration for version 0001",
		},
		{
			name: "missing up migration",
			files: map[string]string{
				"0001_init.down.sql": "SELECT 1;",
			},
			wantErr: true,
			errMsg:  "missing up migration for version 0001",
		},
		{
			name: "malformed filename",
			files: map[string]string{
				"0001_init.up.sql":   "SELECT 1;",
				"0001_init.down.sql": "SELECT 1;",
				"002_second.up.sql":  "SELECT 2;", // 3 digits
			},
			wantErr: true,
			errMsg:  "malformed migration filename \"002_second.up.sql\"",
		},
		{
			name: "skipped version gap",
			files: map[string]string{
				"0001_init.up.sql":        "SELECT 1;",
				"0001_init.down.sql":      "SELECT 1;",
				"0003_add_field.up.sql":   "SELECT 3;",
				"0003_add_field.down.sql": "SELECT 3;",
			},
			wantErr: true,
			errMsg:  "migration sequence gap: expected version 2, but got version 3 (offending file: \"0003_add_field.up.sql\")",
		},
		{
			name: "mismatched names for same version",
			files: map[string]string{
				"0001_init.up.sql":   "SELECT 1;",
				"0001_other.down.sql": "SELECT 1;",
			},
			wantErr: true,
			errMsg:  "migration name mismatch for version 0001: \"0001_init.up.sql\" vs \"0001_other.down.sql\"",
		},
		{
			name: "duplicate up migration version",
			files: map[string]string{
				"0001_init.up.sql":  "SELECT 1;",
				"0001_other.up.sql": "SELECT 2;",
			},
			wantErr: true,
			errMsg:  "duplicate up migration for version 0001",
		},
		{
			name: "no migrations found",
			files:   map[string]string{},
			wantErr: true,
			errMsg:  "no migrations found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for path, content := range tt.files {
				fsys[path] = &fstest.MapFile{Data: []byte(content)}
			}

			err := ValidateFS(fsys)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errMsg)
			}
		})
	}
}

type mockDirEntry struct {
	name string
}

func (e mockDirEntry) Name() string               { return e.name }
func (e mockDirEntry) IsDir() bool                { return false }
func (e mockDirEntry) Type() fs.FileMode          { return 0 }
func (e mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

type mockFS struct {
	entries []fs.DirEntry
}

func (m *mockFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (m *mockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return m.entries, nil
}

func TestValidateFS_NonMonotonic(t *testing.T) {
	// A custom filesystem where files are returned out of order (non-monotonic version ordering)
	fsys := &mockFS{
		entries: []fs.DirEntry{
			mockDirEntry{name: "0002_init.up.sql"},
			mockDirEntry{name: "0001_init.up.sql"},
		},
	}

	err := ValidateFS(fsys)
	if err == nil {
		t.Fatal("expected error for non-monotonic version ordering")
	}
	expectedErr := "non-monotonic migration ordering"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestValidateEmbedded(t *testing.T) {
	t.Run("matching files", func(t *testing.T) {
		tempDir := t.TempDir()
		upContent := "CREATE TABLE t1;"
		downContent := "DROP TABLE t1;"

		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.up.sql"), []byte(upContent), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.down.sql"), []byte(downContent), 0600); err != nil {
			t.Fatal(err)
		}

		embedFS := fstest.MapFS{
			"0001_init.up.sql":   &fstest.MapFile{Data: []byte(upContent)},
			"0001_init.down.sql": &fstest.MapFile{Data: []byte(downContent)},
		}

		if err := ValidateEmbedded(tempDir, embedFS); err != nil {
			t.Fatalf("expected matching to succeed, got error: %v", err)
		}
	})

	t.Run("missing file on disk", func(t *testing.T) {
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.up.sql"), []byte("up"), 0600); err != nil {
			t.Fatal(err)
		}

		embedFS := fstest.MapFS{
			"0001_init.up.sql":   &fstest.MapFile{Data: []byte("up")},
			"0001_init.down.sql": &fstest.MapFile{Data: []byte("down")},
		}

		err := ValidateEmbedded(tempDir, embedFS)
		if err == nil {
			t.Fatal("expected error when file is missing on disk")
		}
		if !strings.Contains(err.Error(), "is embedded but does not exist on disk") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing file in embedded FS", func(t *testing.T) {
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.up.sql"), []byte("up"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.down.sql"), []byte("down"), 0600); err != nil {
			t.Fatal(err)
		}

		embedFS := fstest.MapFS{
			"0001_init.up.sql": &fstest.MapFile{Data: []byte("up")},
		}

		err := ValidateEmbedded(tempDir, embedFS)
		if err == nil {
			t.Fatal("expected error when file is missing in embedded FS")
		}
		if !strings.Contains(err.Error(), "exists on disk but is not embedded") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("content mismatch", func(t *testing.T) {
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tempDir, "0001_init.up.sql"), []byte("up disk"), 0600); err != nil {
			t.Fatal(err)
		}

		embedFS := fstest.MapFS{
			"0001_init.up.sql": &fstest.MapFile{Data: []byte("up embed")},
		}

		err := ValidateEmbedded(tempDir, embedFS)
		if err == nil {
			t.Fatal("expected error when content mismatches")
		}
		if !strings.Contains(err.Error(), "mismatch in content for migration file") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

