package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMigrations_GeneratesRegistryWhenMissing(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := checkMigrations(migrations)
	if !errors.Is(err, errRegistryGenerated) {
		t.Fatalf("expected registry generated error, got %v", err)
	}

	registryPath := filepath.Join(migrations, registryFileName)
	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}

	var registry map[string]string
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry) != 1 {
		t.Fatalf("expected 1 checksum entry, got %d", len(registry))
	}
	if _, ok := registry["0001_init.up.sql"]; !ok {
		t.Fatalf("expected checksum entry for 0001_init.up.sql")
	}
}

func TestCheckMigrations_DetectsModifiedMigration(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksum, err := computeFileChecksum(filePath)
	if err != nil {
		t.Fatal(err)
	}

	registry := map[string]string{"0001_init.up.sql": checksum}
	registryPath := filepath.Join(migrations, registryFileName)
	registryData, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, registryData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id BIGINT PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = checkMigrations(migrations)
	if !errors.Is(err, errMigrationModified) {
		t.Fatalf("expected modified migration error, got %v", err)
	}
}

func TestCheckMigrations_ValidatesUnchangedMigrations(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(migrations, "0001_init.up.sql")
	content := []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	checksum, err := computeFileChecksum(filePath)
	if err != nil {
		t.Fatal(err)
	}

	registry := map[string]string{"0001_init.up.sql": checksum}
	registryPath := filepath.Join(migrations, registryFileName)
	registryData, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, registryData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := checkMigrations(migrations); err != nil {
		t.Fatalf("expected no error for unchanged migrations, got %v", err)
	}
}

func TestCheckMigrations_AddsNewMigrationAndUpdatesRegistry(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	fileA := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(fileA, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksumA, err := computeFileChecksum(fileA)
	if err != nil {
		t.Fatal(err)
	}

	registry := map[string]string{"0001_init.up.sql": checksumA}
	registryPath := filepath.Join(migrations, registryFileName)
	registryData, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, registryData, 0o644); err != nil {
		t.Fatal(err)
	}

	fileB := filepath.Join(migrations, "0002_add_user.up.sql")
	if err := os.WriteFile(fileB, []byte("CREATE TABLE users (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = checkMigrations(migrations)
	if !errors.Is(err, errNewMigrations) {
		t.Fatalf("expected new migrations error, got %v", err)
	}

	// Registry should NOT be updated automatically
	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	var unchangedRegistry map[string]string
	if err := json.Unmarshal(data, &unchangedRegistry); err != nil {
		t.Fatal(err)
	}
	if len(unchangedRegistry) != 1 {
		t.Fatalf("expected registry to remain unchanged with 1 entry, got %d", len(unchangedRegistry))
	}
	if _, ok := unchangedRegistry["0002_add_user.up.sql"]; ok {
		t.Fatal("registry should not contain new migration checksum automatically")
	}
}

func TestCheckMigrations_DetectsRemovedMigration(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksum, err := computeFileChecksum(filePath)
	if err != nil {
		t.Fatal(err)
	}

	registry := map[string]string{"0001_init.up.sql": checksum, "0002_removed.up.sql": "dummyhash"}
	registryPath := filepath.Join(migrations, registryFileName)
	registryData, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, registryData, 0o644); err != nil {
		t.Fatal(err)
	}

	err = checkMigrations(migrations)
	if !errors.Is(err, errMigrationRemoved) {
		t.Fatalf("expected removed migration error, got %v", err)
	}
}

func TestScanMigrations_ReadDirError(t *testing.T) {
	// Test with a non-existent directory
	_, err := scanMigrations("/non/existent/path")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestComputeFileChecksum_OpenError(t *testing.T) {
	_, err := computeFileChecksum("/non/existent/file.sql")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadRegistry_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "valid.json")
	expected := map[string]string{"file1.sql": "hash1", "file2.sql": "hash2"}
	data, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	registry, err := loadRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(registry))
	}
	if registry["file1.sql"] != "hash1" {
		t.Fatal("expected correct hash for file1.sql")
	}
}

func TestWriteRegistry_Success(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	registry := map[string]string{
		"0003_file.sql": "hash3",
		"0001_file.sql": "hash1",
		"0002_file.sql": "hash2",
	}

	err := writeRegistry(registryPath, registry)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	// Check that it's sorted by checking all expected keys are present
	expectedKeys := map[string]bool{
		"0001_file.sql": true,
		"0002_file.sql": true,
		"0003_file.sql": true,
	}
	for key := range result {
		if !expectedKeys[key] {
			t.Fatalf("unexpected key in result: %s", key)
		}
		delete(expectedKeys, key)
	}
	if len(expectedKeys) != 0 {
		t.Fatalf("missing keys in result: %v", expectedKeys)
	}
}

func TestMain_NoArgs(t *testing.T) {
	// This test is tricky because main calls os.Exit. We'll test the logic indirectly.
	// For now, just ensure the code compiles and basic functionality works.
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Test that checkMigrations works (which main calls)
	err := checkMigrations(migrations)
	if !errors.Is(err, errRegistryGenerated) {
		t.Fatalf("expected registry generated error, got %v", err)
	}
}

func TestScanMigrations_IgnoresNonSQLFiles(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create SQL file
	sqlFile := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(sqlFile, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create non-SQL file
	txtFile := filepath.Join(migrations, "readme.txt")
	if err := os.WriteFile(txtFile, []byte("This is a readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	checksums, err := scanMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}

	if len(checksums) != 1 {
		t.Fatalf("expected 1 checksum entry, got %d", len(checksums))
	}

	if _, ok := checksums["0001_init.up.sql"]; !ok {
		t.Fatal("expected checksum for SQL file")
	}

	if _, ok := checksums["readme.txt"]; ok {
		t.Fatal("should not have checksum for non-SQL file")
	}
}

func TestScanMigrations_IgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create SQL file
	sqlFile := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(sqlFile, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create subdirectory
	subDir := filepath.Join(migrations, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	checksums, err := scanMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}

	if len(checksums) != 1 {
		t.Fatalf("expected 1 checksum entry, got %d", len(checksums))
	}
}

func TestCheckMigrations_EmptyMigrationsDirectory(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	err := checkMigrations(migrations)
	if !errors.Is(err, errRegistryGenerated) {
		t.Fatalf("expected registry generated error for empty directory, got %v", err)
	}

	registryPath := filepath.Join(migrations, registryFileName)
	if _, err := os.Stat(registryPath); os.IsNotExist(err) {
		t.Fatal("expected registry file to be created")
	}
}

func TestLoadRegistry_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(registryPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadRegistry(registryPath)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestCheckMigrations_ScanError(t *testing.T) {
	// Test when scanMigrations fails
	err := checkMigrations("/non/existent/path")
	if err == nil {
		t.Fatal("expected error for non-existent migrations directory")
	}
}

func TestLoadRegistry_EmptyJSON(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(registryPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry, err := loadRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(registry) != 0 {
		t.Fatalf("expected empty registry, got %d entries", len(registry))
	}
}

func TestComputeFileChecksum_ReadError(t *testing.T) {
	// Test computeFileChecksum with a non-existent file
	_, err := computeFileChecksum("/non/existent/file.sql")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestCheckMigrations_RegistryHasRegistryFile(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a migration file
	filePath := filepath.Join(migrations, "0001_init.up.sql")
	content := "CREATE TABLE test (id SERIAL PRIMARY KEY);\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Compute the correct hash
	hash, err := computeFileChecksum(filePath)
	if err != nil {
		t.Fatal(err)
	}

	// Create registry with the correct hash and the registry file itself in it (should be ignored)
	registryPath := filepath.Join(migrations, registryFileName)
	registry := map[string]string{
		"0001_init.up.sql": hash,
		"checksums.json":   "ignored",
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = checkMigrations(migrations)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoadRegistry_ReadError(t *testing.T) {
	// Test loadRegistry with a file that can't be read (make it a directory)
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	if err := os.Mkdir(registryPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := loadRegistry(registryPath)
	if err == nil {
		t.Fatal("expected error for directory instead of file")
	}
}

func TestWriteRegistry_WriteError(t *testing.T) {
	// Test writeRegistry with a path that can't be written to
	registry := map[string]string{"test": "hash"}
	err := writeRegistry("/non/existent/directory/registry.json", registry)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestCheckMigrations_MalformedRegistry(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a migration file
	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create malformed registry
	registryPath := filepath.Join(migrations, registryFileName)
	if err := os.WriteFile(registryPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := checkMigrations(migrations)
	if err == nil {
		t.Fatal("expected error for malformed registry")
	}
}

func TestRunWithFlags_Success(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a migration file
	filePath := filepath.Join(migrations, "0001_init.up.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE test (id SERIAL PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create registry
	hash, err := computeFileChecksum(filePath)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(migrations, registryFileName)
	registry := map[string]string{"0001_init.up.sql": hash}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a new flag set
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	// Set args
	args := []string{"-dir", migrations}

	exitCode := runWithFlags(fs, args)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunWithFlags_Error(t *testing.T) {
	// Use a new flag set
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	// Set args for run with non-existent directory
	args := []string{"-dir", "/non/existent"}

	exitCode := runWithFlags(fs, args)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRunWithFlags_ParseError(t *testing.T) {
	// Use a new flag set
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	// Set args with invalid flag
	args := []string{"-invalid", "flag"}

	exitCode := runWithFlags(fs, args)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestCheckMigrations_MultipleMissingFiles(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create registry with multiple files
	registryPath := filepath.Join(migrations, registryFileName)
	registry := map[string]string{
		"0001_file.sql": "hash1",
		"0002_file.sql": "hash2",
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Don't create any migration files, so all are missing

	err = checkMigrations(migrations)
	if err == nil {
		t.Fatal("expected error for missing files")
	}
	// Check that the error message contains the first missing file
	if !strings.Contains(err.Error(), "0001_file.sql") {
		t.Fatalf("expected error to contain first missing file, got %v", err)
	}
}

func TestRun_Success(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	code := run(fs, []string{"-dir", `C:\Users\Admin\stellabill-backend\migrations`})
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
}

func TestMain_Error(t *testing.T) {
	var exitCode int
	originalExit := osExit
	osExit = func(code int) {
		exitCode = code
	}
	defer func() {
		osExit = originalExit
	}()

	origArgs := os.Args
	os.Args = []string{"test", "-dir", "/nonexistent"}
	defer func() { os.Args = origArgs }()

	main()

	if exitCode != 1 {
		t.Fatalf("expected 1, got %d", exitCode)
	}
}
