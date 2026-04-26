package reconciliation

// Store is a simple persistence interface for reconciliation reports.
type Store interface {
	SaveReports(reports []Report) error
	ListReports() ([]Report, error)
	DeleteReportsByJobID(jobID string) error
	GetReportsByJobID(jobID string) ([]Report, error)
}
