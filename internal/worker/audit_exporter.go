package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/security"
	"time"

	"go.uber.org/zap"
)

// AuditExporterJob handles the nightly export of audit logs to a data warehouse.
// In this implementation, it exports to a local staging file which a data warehouse
// ingestion tool (like Fluentd, Logstash, or a cron script) will pick up.
type AuditExporterJob struct {
	auditLogPath   string
	exportFilePath string
}

func NewAuditExporterJob(auditLogPath, exportFilePath string) *AuditExporterJob {
	if auditLogPath == "" {
		auditLogPath = "audit.log"
	}
	if exportFilePath == "" {
		exportFilePath = "audit_export_warehouse.jsonl"
	}
	return &AuditExporterJob{
		auditLogPath:   auditLogPath,
		exportFilePath: exportFilePath,
	}
}

// Run executes the export process. It can be called by a cron scheduler nightly.
func (j *AuditExporterJob) Run(ctx context.Context) error {
	security.ProductionLogger().Info("Starting audit log export to data warehouse")

	inFile, err := os.Open(j.auditLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			security.ProductionLogger().Info("No audit log found, nothing to export")
			return nil
		}
		return fmt.Errorf("failed to open audit log: %w", err)
	}
	defer inFile.Close()

	// Use O_TRUNC to overwrite the export file daily, or O_APPEND depending on ingestion strategy.
	outFile, err := os.OpenFile(j.exportFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open export file: %w", err)
	}
	defer outFile.Close()

	scanner := bufio.NewScanner(inFile)
	exportedCount := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		var event audit.AuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			security.ProductionLogger().Warn("Failed to unmarshal audit event", zap.Error(err))
			continue
		}

		// Currently, we want to export feature_flag_toggle events and other critical audit logs.
		// For the data warehouse, we ensure the schema is flat and analytic-friendly.
		if event.Action == "feature_flag_toggle" {
			exportRecord := map[string]interface{}{
				"timestamp":     event.Timestamp.Format(time.RFC3339),
				"actor":         event.Actor,
				"action":        event.Action,
				"resource":      event.Resource,
				"outcome":       event.Outcome,
				"reason":        event.Metadata["reason"],
				"before_enable": event.Metadata["before_enabled"],
				"after_enable":  event.Metadata["after_enabled"],
				"hash":          event.Hash,
			}

			outBytes, err := json.Marshal(exportRecord)
			if err != nil {
				security.ProductionLogger().Warn("Failed to marshal export record", zap.Error(err))
				continue
			}
			
			if _, err := outFile.Write(append(outBytes, '\n')); err != nil {
				return fmt.Errorf("failed to write to export file: %w", err)
			}
			exportedCount++
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading audit log: %w", err)
	}

	security.ProductionLogger().Info("Audit log export completed", zap.Int("exported_count", exportedCount))
	return nil
}
