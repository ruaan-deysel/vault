package storage

import (
	"io"
	"time"
)

type FileInfo struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

type Adapter interface {
	Write(path string, reader io.Reader) error
	Read(path string) (io.ReadCloser, error)
	Delete(path string) error
	List(prefix string) ([]FileInfo, error)
	Stat(path string) (FileInfo, error)
	TestConnection() error
}
