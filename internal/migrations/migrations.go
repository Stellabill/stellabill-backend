package migrations

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	filenameRe = regexp.MustCompile(`^(\d+)_([a-zA-Z0-9][a-zA-Z0-9_-]*)\.(up|down)\.sql$`)
)

type Migration struct {
	Version int64
	Name    string
	UpSQL   string
	DownSQL string
}

func LoadDir(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type partial struct {
		version  int64
		name     string
		upPath   string
		downPath string
	}

	byVersion := map[int64]*partial{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := filenameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}

		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", e.Name())
		}
		name := m[2]
		kind := m[3]

		p := byVersion[version]
		if p == nil {
			p = &partial{version: version, name: name}
			byVersion[version] = p
		}
		if p.name != name {
			return nil, fmt.Errorf("conflicting migration names for version %d: %q vs %q", version, p.name, name)
		}

		fullPath := filepath.Join(dir, e.Name())
		switch kind {
		case "up":
			if p.upPath != "" {
				return nil, fmt.Errorf("duplicate up migration for version %d", version)
			}
			p.upPath = fullPath
		case "down":
			if p.downPath != "" {
				return nil, fmt.Errorf("duplicate down migration for version %d", version)
			}
			p.downPath = fullPath
		}
	}

	if len(byVersion) == 0 {
		return nil, fmt.Errorf("no migrations found in %q", dir)
	}

	var versions []int64
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	out := make([]Migration, 0, len(versions))
	for _, v := range versions {
		p := byVersion[v]
		if p.upPath == "" || p.downPath == "" {
			missing := "up"
			if p.upPath != "" {
				missing = "down"
			}
			return nil, fmt.Errorf("missing %s migration for version %d (%s)", missing, p.version, p.name)
		}

		upSQL, err := readSQLFile(p.upPath)
		if err != nil {
			return nil, err
		}
		downSQL, err := readSQLFile(p.downPath)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{
			Version: v,
			Name:    p.name,
			UpSQL:   upSQL,
			DownSQL: downSQL,
		})
	}
	return out, nil
}

func readSQLFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("missing migration file %q", path)
		}
		return "", err
	}
	sql := strings.TrimSpace(string(b))
	if sql == "" {
		return "", fmt.Errorf("empty migration file %q", path)
	}
	return sql, nil
}

func FindByVersion(migs []Migration, version int64) (Migration, bool) {
	for _, m := range migs {
		if m.Version == version {
			return m, true
		}
	}
	return Migration{}, false
}
