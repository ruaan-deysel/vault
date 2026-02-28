package storage

import (
	"fmt"
	"io"
	"net"
	"path/filepath"

	"github.com/hirochachacha/go-smb2"
)

type SMBConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Share    string `json:"share"`
	BasePath string `json:"base_path"`
}

type SMBAdapter struct {
	config SMBConfig
}

func NewSMBAdapter(config SMBConfig) (*SMBAdapter, error) {
	if config.Port == 0 {
		config.Port = 445
	}
	return &SMBAdapter{config: config}, nil
}

func (s *SMBAdapter) connect() (*smb2.Share, *smb2.Session, net.Conn, error) {
	addr := net.JoinHostPort(s.config.Host, fmt.Sprintf("%d", s.config.Port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial: %w", err)
	}

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     s.config.User,
			Password: s.config.Password,
		},
	}

	session, err := d.Dial(conn)
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("smb dial: %w", err)
	}

	share, err := session.Mount(s.config.Share)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, nil, nil, fmt.Errorf("mount share: %w", err)
	}

	return share, session, conn, nil
}

func (s *SMBAdapter) fullPath(path string) string {
	return filepath.Join(s.config.BasePath, filepath.Clean(path))
}

func (s *SMBAdapter) Write(path string, reader io.Reader) error {
	share, session, conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer session.Logoff()
	defer share.Umount()

	full := s.fullPath(path)
	share.MkdirAll(filepath.Dir(full), 0755)

	f, err := share.Create(full)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	return err
}

func (s *SMBAdapter) Read(path string) (io.ReadCloser, error) {
	share, session, conn, err := s.connect()
	if err != nil {
		return nil, err
	}

	f, err := share.Open(s.fullPath(path))
	if err != nil {
		share.Umount()
		session.Logoff()
		conn.Close()
		return nil, err
	}
	return &smbReadCloser{file: f, share: share, session: session, conn: conn}, nil
}

type smbReadCloser struct {
	file    *smb2.File
	share   *smb2.Share
	session *smb2.Session
	conn    net.Conn
}

func (r *smbReadCloser) Read(p []byte) (int, error) { return r.file.Read(p) }
func (r *smbReadCloser) Close() error {
	r.file.Close()
	r.share.Umount()
	r.session.Logoff()
	return r.conn.Close()
}

func (s *SMBAdapter) Delete(path string) error {
	share, session, conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer session.Logoff()
	defer share.Umount()
	return share.Remove(s.fullPath(path))
}

func (s *SMBAdapter) List(prefix string) ([]FileInfo, error) {
	share, session, conn, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer session.Logoff()
	defer share.Umount()

	entries, err := share.ReadDir(s.fullPath(prefix))
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, e := range entries {
		files = append(files, FileInfo{
			Path:    filepath.Join(prefix, e.Name()),
			Size:    e.Size(),
			ModTime: e.ModTime(),
			IsDir:   e.IsDir(),
		})
	}
	return files, nil
}

func (s *SMBAdapter) Stat(path string) (FileInfo, error) {
	share, session, conn, err := s.connect()
	if err != nil {
		return FileInfo{}, err
	}
	defer conn.Close()
	defer session.Logoff()
	defer share.Umount()

	info, err := share.Stat(s.fullPath(path))
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

func (s *SMBAdapter) TestConnection() error {
	share, session, conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer session.Logoff()
	defer share.Umount()

	_, err = share.ReadDir(s.config.BasePath)
	return err
}

var _ Adapter = (*SMBAdapter)(nil)
