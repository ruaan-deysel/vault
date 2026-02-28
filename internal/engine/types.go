package engine

type BackupItem struct {
	Name     string
	Type     string // "container" or "vm"
	Settings map[string]any
}

type BackupResult struct {
	ItemName string
	Success  bool
	Error    string
	Files    []BackupFile
}

type BackupFile struct {
	Name string
	Size int64
}

type ProgressFunc func(item string, percent int, message string)

type Handler interface {
	Backup(item BackupItem, dest string, progress ProgressFunc) (*BackupResult, error)
	Restore(item BackupItem, source string, progress ProgressFunc) error
	ListItems() ([]BackupItem, error)
}
