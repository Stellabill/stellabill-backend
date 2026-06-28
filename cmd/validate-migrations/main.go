package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/lib/pq"

	internalMigs "stellarbill-backend/internal/migrations"
	"stellarbill-backend/migrations"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print pending SQL statements and exit without applying")
	flag.Parse()

	// 1. Validate the disk migrations directory strictly
	diskFS := os.DirFS("migrations")
	if err := internalMigs.ValidateFS(diskFS); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed for disk migrations: %v\n", err)
		os.Exit(1)
	}

	// 2. Validate the embedded migrations strictly
	if err := internalMigs.ValidateFS(migrations.FS); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed for embedded migrations: %v\n", err)
		os.Exit(1)
	}

	// 3. Verify embedded migrations exactly match disk migrations
	if err := internalMigs.ValidateEmbedded("migrations", migrations.FS); err != nil {
		fmt.Fprintf(os.Stderr, "Embedded migrations mismatch with disk: %v\n", err)
		os.Exit(1)
	}

	// 4. Ensure original LoadDir and sequence verification checks pass
	migs, err := internalMigs.LoadDir("migrations")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load migrations: %v\n", err)
		os.Exit(1)
	}

	if len(migs) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found.")
		os.Exit(1)
	}

	if err := internalMigs.ValidateSequence(migs); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if !*dryRun {
		fmt.Println("Migrations are sequential and valid.")
		return
	}

	// Dry-run: connect to DB, print pending SQL, roll back.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required for --dry-run")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	runner := internalMigs.Runner{DB: db}
	result, err := runner.DryRun(context.Background(), migs, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dry-run failed: %v\n", err)
		os.Exit(1)
	}

	if len(result.Pending) == 0 {
		fmt.Println("-- [dry-run] no pending migrations")
	} else {
		fmt.Printf("-- [dry-run] %d pending migration(s) listed above; no changes applied\n", len(result.Pending))
	}
}
