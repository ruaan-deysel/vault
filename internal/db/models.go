package db

import "time"

type Job struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
	Schedule        string    `json:"schedule"`
	BackupTypeChain string    `json:"backup_type_chain"`
	RetentionCount  int       `json:"retention_count"`
	RetentionDays   int       `json:"retention_days"`
	Compression     string    `json:"compression"`
	ContainerMode   string    `json:"container_mode"`
	PreScript       string    `json:"pre_script"`
	PostScript      string    `json:"post_script"`
	NotifyOn        string    `json:"notify_on"`
	StorageDestID   int64     `json:"storage_dest_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type JobItem struct {
	ID       int64  `json:"id"`
	JobID    int64  `json:"job_id"`
	ItemType string `json:"item_type"`
	ItemName string `json:"item_name"`
	ItemID   string `json:"item_id"`
	Settings string `json:"settings"`
}

type JobRun struct {
	ID          int64     `json:"id"`
	JobID       int64     `json:"job_id"`
	Status      string    `json:"status"`
	BackupType  string    `json:"backup_type"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Log         string    `json:"log"`
	ItemsTotal  int       `json:"items_total"`
	ItemsDone   int       `json:"items_done"`
	ItemsFailed int       `json:"items_failed"`
	SizeBytes   int64     `json:"size_bytes"`
}

type RestorePoint struct {
	ID          int64     `json:"id"`
	JobRunID    int64     `json:"job_run_id"`
	JobID       int64     `json:"job_id"`
	BackupType  string    `json:"backup_type"`
	StoragePath string    `json:"storage_path"`
	Metadata    string    `json:"metadata"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type StorageDestination struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Config    string    `json:"config"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
