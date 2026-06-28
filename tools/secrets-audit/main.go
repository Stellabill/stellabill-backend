package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"stellarbill-backend/internal/secrets"
)

type reportItem struct {
	Name              string    `json:"name"`
	Owner             string    `json:"owner"`
	Source            string    `json:"source"`
	RotationCadence   string    `json:"rotation_cadence"`
	LastRotatedAt     time.Time `json:"last_rotated_at"`
	NextRotationDueAt time.Time `json:"next_rotation_due_at"`
	Status            string    `json:"status"`
	Message           string    `json:"message"`
}

func main() {
	var (
		manifestPath = flag.String("manifest", os.Getenv("SECRETS_AUDIT_MANIFEST"), "path to secrets metadata manifest JSON")
		asJSON       = flag.Bool("json", false, "emit JSON report")
		dryRun       = flag.Bool("dry-run", false, "validate rotation due dates without making changes")
		nowStr       = flag.String("now", "", "override current time in RFC3339")
	)
	flag.Parse()

	now := time.Now().UTC()
	if *nowStr != "" {
		t, err := time.Parse(time.RFC3339, *nowStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --now: %v\n", err)
			os.Exit(2)
		}
		now = t.UTC()
	}

	provider := secrets.NewDefaultProvider()

	var manifest secrets.MetadataProvider
	if strings.TrimSpace(*manifestPath) != "" {
		manifest = secrets.NewManifestMetadataProvider(*manifestPath)
	}

	keys := flag.Args()
	if len(keys) == 0 {
		keys = []string{
			"JWT_SECRET",
			"JWKS_SECRET",
			"DB_PASSWORD",
			"WEBHOOK_SECRET",
			"ADMIN_SIGNATURE_SECRET",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var report []reportItem
	failed := false

	for _, key := range keys {
		md, err := loadMetadata(ctx, provider, manifest, key)
		item := reportItem{
			Name:   key,
			Source: provider.Name(),
		}

		if err != nil {
			item.Status = "unknown"
			item.Message = err.Error()
			failed = true
			report = append(report, item)
			continue
		}

		item.Owner = md.Owner
		item.Source = md.Source
		item.RotationCadence = md.RotationCadence
		item.LastRotatedAt = md.LastRotatedAt
		item.NextRotationDueAt = md.NextRotationDueAt

		if md.NextRotationDueAt.IsZero() {
			item.Status = "unknown"
			item.Message = "missing rotation due date metadata"
			failed = true
		} else if now.After(md.NextRotationDueAt) {
			item.Status = "expired"
			item.Message = fmt.Sprintf("past due since %s", md.NextRotationDueAt.Format(time.RFC3339))
			failed = true
		} else {
			item.Status = "ok"
			item.Message = fmt.Sprintf("due in %s", md.NextRotationDueAt.Sub(now).Round(time.Second))
		}

		report = append(report, item)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"dry_run": *dryRun,
			"now":     now.Format(time.RFC3339),
			"items":   report,
		})
	} else {
		for _, item := range report {
			fmt.Printf("%-24s %-10s %-20s %s\n", item.Name, item.Status, item.Owner, item.Message)
		}
	}

	if failed {
		os.Exit(1)
	}
}

func loadMetadata(ctx context.Context, provider secrets.Provider, manifest secrets.MetadataProvider, key string) (secrets.SecretMetadata, error) {
	if mp, ok := provider.(secrets.MetadataProvider); ok {
		md, err := mp.Metadata(ctx, key)
		if err == nil {
			return md, nil
		}
		if !errors.Is(err, secrets.ErrMetadataNotFound) && !errors.Is(err, secrets.ErrMetadataNotSupported) {
			return secrets.SecretMetadata{}, err
		}
	}

	if manifest != nil {
		md, err := manifest.Metadata(ctx, key)
		if err == nil {
			return md, nil
		}
		return secrets.SecretMetadata{}, err
	}

	return secrets.SecretMetadata{}, secrets.ErrMetadataNotFound
}