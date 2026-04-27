package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const registryFileName = "checksums.json"

var (
	errRegistryGenerated = errors.New("registry generated")
	errNewMigrations     = errors.New("new migrations added")
	errMigrationModified = errors.New("migration modified")
	errMigrationRemoved  = errors.New("migration removed")
)

var osExit = os.Exit

func main() {
	osExit(run(flag.CommandLine, os.Args[1:]))
}

func run(fs *flag.FlagSet, args []string) int {
	return runWithFlags(fs, args)
}

func runWithFlags(fs *flag.FlagSet, args []string) int {
	dir := fs.String("dir", "migrations", "migrations directory")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := checkMigrations(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func checkMigrations(dir string) error {
	current, err := scanMigrations(dir)
	if err != nil {
		return err
	}

	registryPath := filepath.Join(dir, registryFileName)
	registry, err := loadRegistry(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := writeRegistry(registryPath, current); err != nil {
				return err
			}
			return fmt.Errorf("%w: generated %s; commit it with new migration files", errRegistryGenerated, registryPath)
		}
		return err
	}

	newFiles := make([]string, 0, len(current))
	for name, hash := range current {
		oldHash, ok := registry[name]
		if !ok {
			newFiles = append(newFiles, name)
			registry[name] = hash
			continue
		}
		if oldHash != hash {
			return fmt.Errorf("%w: %s\nExisting migrations must not be edited. Create a new migration instead", errMigrationModified, filepath.Join(dir, name))
		}
	}

	missingFiles := make([]string, 0)
	for name := range registry {
		if _, ok := current[name]; !ok {
			if name == registryFileName {
				continue
			}
			missingFiles = append(missingFiles, name)
		}
	}
	if len(missingFiles) > 0 {
		sort.Strings(missingFiles)
		return fmt.Errorf("%w: %s\nExisting migrations must not be removed. Restore the file or add a new migration instead", errMigrationRemoved, filepath.Join(dir, missingFiles[0]))
	}

	if len(newFiles) > 0 {
		sort.Strings(newFiles)
		return fmt.Errorf("%w: new migrations detected: %s\nRun this script locally to update %s, then commit the changes", errNewMigrations, strings.Join(newFiles, ", "), registryFileName)
	}

	fmt.Println("Migration registry validated.")
	return nil
}

func scanMigrations(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		hash, err := computeFileChecksum(path)
		if err != nil {
			return nil, err
		}
		checksums[entry.Name()] = hash
	}

	return checksums, nil
}

func computeFileChecksum(path string) (string, error) {
	file, err := os.Open(path) // #nosec G304
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func loadRegistry(path string) (map[string]string, error) {
	data, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return nil, err
	}

	var registry map[string]string
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	if registry == nil {
		registry = make(map[string]string)
	}
	return registry, nil
}

func writeRegistry(path string, registry map[string]string) error {
	keys := make([]string, 0, len(registry))
	for name := range registry {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	ordered := make(map[string]string, len(registry))
	for _, name := range keys {
		ordered[name] = registry[name]
	}

	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

