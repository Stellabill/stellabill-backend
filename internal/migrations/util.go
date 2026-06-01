package migrations

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	// StrictFilenameRe matches exactly zero-padded 4-digit version, name (alphanumeric, underscores, hyphens), kind (up/down), and .sql extension.
	StrictFilenameRe = regexp.MustCompile(`^(\d{4})_([a-zA-Z0-9][a-zA-Z0-9_-]*)\.(up|down)\.sql$`)
)

// RedactDatabaseURL removes password/userinfo from DSNs for safe logging.
func RedactDatabaseURL(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "<invalid database url>"
	}
	if u.User != nil {
		user := u.User.Username()
		if user != "" {
			u.User = url.User(user)
		} else {
			u.User = nil
		}
	}
	s := u.String()
	if strings.Contains(s, "@") && strings.Contains(s, "://") {
		return s
	}
	return s
}

// ParseMigrationFilename parses a migration filename and returns the version, name, kind (up/down), or an error.
func ParseMigrationFilename(filename string) (int64, string, string, error) {
	m := StrictFilenameRe.FindStringSubmatch(filename)
	if m == nil {
		return 0, "", "", fmt.Errorf("filename %q does not match format NNNN_name.(up|down).sql", filename)
	}
	version, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || version <= 0 {
		return 0, "", "", fmt.Errorf("invalid version %q in filename %q", m[1], filename)
	}
	return version, m[2], m[3], nil
}

// ValidateFS validates all migration files in the provided filesystem.
func ValidateFS(fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	type migrationFile struct {
		version  int64
		name     string
		kind     string
		filename string
	}

	var files []migrationFile
	var lastVersion int64 = 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".go") {
			continue
		}

		version, mName, kind, err := ParseMigrationFilename(name)
		if err != nil {
			return fmt.Errorf("malformed migration filename %q: %w", name, err)
		}

		// Enforce alphabetical order matches monotonic numeric order
		if version < lastVersion {
			return fmt.Errorf("non-monotonic migration ordering: filename %q has version %d, which is less than preceding version %d", name, version, lastVersion)
		}
		lastVersion = version

		files = append(files, migrationFile{
			version:  version,
			name:     mName,
			kind:     kind,
			filename: name,
		})
	}

	if len(files) == 0 {
		return fmt.Errorf("no migrations found")
	}

	type versionGroup struct {
		upFile   *migrationFile
		downFile *migrationFile
	}
	byVersion := make(map[int64]*versionGroup)
	for i := range files {
		f := &files[i]
		g, exists := byVersion[f.version]
		if !exists {
			g = &versionGroup{}
			byVersion[f.version] = g
		}

		if f.kind == "up" {
			if g.upFile != nil {
				return fmt.Errorf("duplicate up migration for version %04d (found %q and %q)", f.version, g.upFile.filename, f.filename)
			}
			g.upFile = f
		} else {
			if g.downFile != nil {
				return fmt.Errorf("duplicate down migration for version %04d (found %q and %q)", f.version, g.downFile.filename, f.filename)
			}
			g.downFile = f
		}
	}

	var sortedVersions []int64
	for v := range byVersion {
		sortedVersions = append(sortedVersions, v)
	}
	sort.Slice(sortedVersions, func(i, j int) bool {
		return sortedVersions[i] < sortedVersions[j]
	})

	for i, v := range sortedVersions {
		expected := int64(i + 1)
		if v != expected {
			// Find the offending file name for the error message
			offendingFile := ""
			g := byVersion[v]
			if g.upFile != nil {
				offendingFile = g.upFile.filename
			} else if g.downFile != nil {
				offendingFile = g.downFile.filename
			}
			return fmt.Errorf("migration sequence gap: expected version %d, but got version %d (offending file: %q)", expected, v, offendingFile)
		}

		g := byVersion[v]
		if g.upFile == nil {
			return fmt.Errorf("missing up migration for version %04d (found down migration %q)", v, g.downFile.filename)
		}
		if g.downFile == nil {
			return fmt.Errorf("missing down migration for version %04d (found up migration %q)", v, g.upFile.filename)
		}

		if g.upFile.name != g.downFile.name {
			return fmt.Errorf("migration name mismatch for version %04d: %q vs %q", v, g.upFile.filename, g.downFile.filename)
		}
	}

	return nil
}

// ValidateEmbedded verifies that the SQL files in the embedded FS exactly match the SQL files on disk in diskDir.
func ValidateEmbedded(diskDir string, embedFS fs.FS) error {
	embedEntries, err := fs.ReadDir(embedFS, ".")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations: %w", err)
	}

	diskEntries, err := os.ReadDir(diskDir)
	if err != nil {
		return fmt.Errorf("failed to read disk migrations from %q: %w", diskDir, err)
	}

	embedFiles := make(map[string]bool)
	for _, e := range embedEntries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			embedFiles[e.Name()] = true
		}
	}

	diskFiles := make(map[string]bool)
	for _, e := range diskEntries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			diskFiles[e.Name()] = true
		}
	}

	for f := range diskFiles {
		if !embedFiles[f] {
			return fmt.Errorf("migration file %q exists on disk but is not embedded", f)
		}
	}
	for f := range embedFiles {
		if !diskFiles[f] {
			return fmt.Errorf("migration file %q is embedded but does not exist on disk", f)
		}
	}

	for f := range diskFiles {
		diskContent, err := os.ReadFile(filepath.Join(diskDir, f))
		if err != nil {
			return fmt.Errorf("failed to read disk migration file %q: %w", f, err)
		}

		embedContent, err := fs.ReadFile(embedFS, f)
		if err != nil {
			return fmt.Errorf("failed to read embedded migration file %q: %w", f, err)
		}

		if string(diskContent) != string(embedContent) {
			return fmt.Errorf("mismatch in content for migration file %q", f)
		}
	}

	return nil
}

