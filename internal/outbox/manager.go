package outbox

import (
	"database/sql"
	"fmt"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/logger"
)

// Manager handles the outbox system lifecycle
type Manager struct {
	service *Service
	db      *sql.DB
}

// NewManager creates a new outbox manager
func NewManager(db *sql.DB, cfg config.Config) (*Manager, error) {
	// Convert config to outbox service config
	serviceConfig := ServiceConfig{
		DispatcherConfig: DispatcherConfig{
			PollInterval:       cfg.Outbox.GetPollInterval(),
			BatchSize:          cfg.Outbox.BatchSize,
			MaxRetries:         cfg.Outbox.MaxRetries,
			RetryBackoffFactor: cfg.Outbox.RetryBackoffFactor,
			CleanupInterval:    cfg.Outbox.GetCleanupInterval(),
			CompletedEventTTL:  cfg.Outbox.GetCompletedEventTTL(),
			ProcessingTimeout:  cfg.Outbox.GetProcessingTimeout(),
		},
		PublisherType: cfg.Outbox.PublisherType,
		HTTPEndpoint:  cfg.Outbox.HTTPEndpoint,
	}

	service, err := NewService(db, serviceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create outbox service: %w", err)
	}

	return &Manager{
		service: service,
		db:      db,
	}, nil
}

// Start starts the outbox system
func (m *Manager) Start() error {
	logger.SafePrintf("Starting outbox manager...")
	
	// Run database migrations
	if err := m.runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	
	// Start the dispatcher
	if err := m.service.Start(); err != nil {
		return fmt.Errorf("failed to start outbox service: %w", err)
	}
	
	logger.SafePrintf("Outbox manager started successfully")
	return nil
}

// Stop stops the outbox system
func (m *Manager) Stop() error {
	logger.SafePrintf("Stopping outbox manager...")
	
	if err := m.service.Stop(); err != nil {
		return fmt.Errorf("failed to stop outbox service: %w", err)
	}
	
	logger.SafePrintf("Outbox manager stopped")
	return nil
}

// GetService returns the outbox service
func (m *Manager) GetService() *Service {
	return m.service
}

// GetManager returns the outbox manager
func (m *Manager) GetManager() *OutboxManager {
	return NewOutboxManager(m.service)
}

// Health checks the health of the outbox system
func (m *Manager) Health() error {
	return m.service.Health()
}

// runMigrations runs the necessary database migrations
func (m *Manager) runMigrations() error {
	logger.SafePrintf("Running outbox migrations...")
	
	// Check if outbox table exists
	var exists bool
	err := m.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_name = 'outbox_events'
		)
	`).Scan(&exists)
	
	if err != nil {
		return fmt.Errorf("failed to check if outbox table exists: %w", err)
	}
	
	if !exists {
		logger.SafePrintf("Creating outbox table...")
		if err := m.createOutboxTable(); err != nil {
			return fmt.Errorf("failed to create outbox table: %w", err)
		}
	}
	
	return nil
}

// createOutboxTable creates the outbox events table
func (m *Manager) createOutboxTable() error {
	// Note: This is a simplified version. In production, you would want to use
	// a proper migration tool like golang-migrate or flyway
	query := `
		CREATE TABLE outbox_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_type VARCHAR(255) NOT NULL,
			event_data JSONB NOT NULL,
			aggregate_id VARCHAR(255),
			aggregate_type VARCHAR(100),
			occurred_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
			retry_count INTEGER NOT NULL DEFAULT 0,
			max_retries INTEGER NOT NULL DEFAULT 3,
			next_retry_at TIMESTAMP WITH TIME ZONE,
			error_message TEXT,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			version INTEGER NOT NULL DEFAULT 1
		);
		
		CREATE INDEX idx_outbox_events_status ON outbox_events(status);
		CREATE INDEX idx_outbox_events_next_retry ON outbox_events(next_retry_at) WHERE next_retry_at IS NOT NULL;
		CREATE INDEX idx_outbox_events_aggregate ON outbox_events(aggregate_type, aggregate_id);
		CREATE INDEX idx_outbox_events_occurred_at ON outbox_events(occurred_at);
		
		-- Create trigger to update updated_at timestamp
		CREATE OR REPLACE FUNCTION update_outbox_updated_at()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ language 'plpgsql';
		
		CREATE TRIGGER trigger_update_outbox_updated_at
			BEFORE UPDATE ON outbox_events
			FOR EACH ROW
			EXECUTE FUNCTION update_outbox_updated_at();
	`
	
	_, err := m.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create outbox table: %w", err)
	}
	
	logger.SafePrintf("Outbox table created successfully")
	return nil
}

// GetStats returns outbox statistics for monitoring
func (m *Manager) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	
	// Get pending events count
	pendingCount, err := m.service.GetPendingEventsCount()
	if err != nil {
		return nil, fmt.Errorf("failed to get pending events count: %w", err)
	}
	stats["pending_events"] = pendingCount
	
	// Get dispatcher status
	stats["dispatcher_running"] = m.service.IsRunning()
	
	// Get database health
	if err := m.db.Ping(); err != nil {
		stats["database_health"] = "unhealthy"
		stats["database_error"] = err.Error()
	} else {
		stats["database_health"] = "healthy"
	}
	
	return stats, nil
}
