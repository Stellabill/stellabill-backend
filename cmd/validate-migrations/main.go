package main

import (
	"fmt"
	"os"

	internalMigs "stellarbill-backend/internal/migrations"
	"stellarbill-backend/migrations"
)

func main() {
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

	fmt.Println("Migrations are sequential and valid.")
}

