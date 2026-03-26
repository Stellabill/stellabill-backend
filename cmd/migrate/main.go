package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
	"stellarbill-backend/internal/migrations"
)

func main() {
	var (
		dir         = flag.String("dir", "migrations", "migrations directory")
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "database connection URL (or set DATABASE_URL)")
		dryRun      = flag.Bool("dry-run", false, "print what would run without changing the database")
		timeout     = flag.Duration("timeout", 30*time.Second, "command timeout")
	)
	flag.Parse()

	if flag.NArg() < 1 {
		usage("missing command")
	}
	cmd := flag.Arg(0)

	migs, err := migrations.LoadDir(*dir)
	if err != nil {
		fatal(err)
	}

	if *dryRun {
		switch cmd {
		case "up":
			for _, m := range migs {
				fmt.Printf("would apply: %04d_%s.up.sql\n", m.Version, m.Name)
			}
			return
		case "down":
			last := migs[len(migs)-1]
			fmt.Printf("would rollback: %04d_%s.down.sql\n", last.Version, last.Name)
			return
		default:
			usage("dry-run is only supported for up/down")
		}
	}

	if *databaseURL == "" {
		usage("missing -database-url (or DATABASE_URL)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, err := sql.Open("postgres", *databaseURL)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	r := migrations.Runner{DB: db}

	switch cmd {
	case "up":
		applied, err := r.Up(ctx, migs)
		if err != nil {
			fatal(err)
		}
		if len(applied) == 0 {
			fmt.Println("no migrations to apply")
			return
		}
		for _, m := range applied {
			fmt.Printf("applied: %04d_%s\n", m.Version, m.Name)
		}
	case "down":
		m, err := r.Down(ctx, migs)
		if err != nil {
			fatal(err)
		}
		if m == nil {
			fmt.Println("no migrations to rollback")
			return
		}
		fmt.Printf("rolled back: %04d_%s\n", m.Version, m.Name)
	case "status":
		applied, err := r.Applied(ctx)
		if err != nil {
			fatal(err)
		}
		appliedSet := map[int64]struct{}{}
		for _, a := range applied {
			appliedSet[a.Version] = struct{}{}
		}
		for _, m := range migs {
			_, ok := appliedSet[m.Version]
			if ok {
				fmt.Printf("applied: %04d_%s\n", m.Version, m.Name)
			} else {
				fmt.Printf("pending: %04d_%s\n", m.Version, m.Name)
			}
		}
	case "version":
		applied, err := r.Applied(ctx)
		if err != nil {
			fatal(err)
		}
		if len(applied) == 0 {
			fmt.Println("0")
			return
		}
		fmt.Println(applied[len(applied)-1].Version)
	default:
		usage("unknown command: " + cmd)
	}
}

func usage(msg string) {
	if msg != "" {
		_, _ = fmt.Fprintln(os.Stderr, msg)
	}
	_, _ = fmt.Fprintln(os.Stderr, "Usage:")
	_, _ = fmt.Fprintln(os.Stderr, "  migrate [flags] <up|down|status|version>")
	_, _ = fmt.Fprintln(os.Stderr, "")
	flag.PrintDefaults()
	os.Exit(2)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
