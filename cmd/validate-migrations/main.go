package main

import (
	"fmt"
	"os"

	"stellarbill-backend/internal/migrations"
)

func main() {
	migs, err := migrations.LoadDir("migrations")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load migrations: %v\n", err)
		os.Exit(1)
	}

	if len(migs) == 0 {
		fmt.Println("No migrations found.")
		return
	}

	if err := migrations.ValidateSequence(migs); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	fmt.Println("Migrations are sequential and valid.")
}
