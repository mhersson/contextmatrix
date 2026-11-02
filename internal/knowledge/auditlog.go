// Package knowledge is ContextMatrix's client for knowledge providers:
// read-only MCP servers that answer questions about a product.
package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	DefaultAuditMaxBytes int64 = 10 << 20
	DefaultAuditKeep           = 3
)

// Entry is one provider call as written to the audit log.
type Entry struct {
	Time       time.Time       `json:"time"`
	Provider   string          `json:"provider"`
	Scope      string          `json:"scope"`
	Project    string          `json:"project"`
	Caller     string          `json:"caller"`
	Tool       string          `json:"tool"`
	Arguments  map[string]any  `json:"arguments"`
	Answer     json.RawMessage `json:"answer"`
	DurationMS int64           `json:"duration_ms"`
	Error      string          `json:"error,omitempty"`
	Cached     bool            `json:"cached,omitempty"`
}

// AuditLog appends one JSON line per Entry to a size-rotated file.
type AuditLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	f        *os.File
	size     int64
}

// NewAuditLog opens path for append, creating its parent directory (0700) and
// the file (0600) as needed.
func NewAuditLog(path string, maxBytes int64, keep int) (*AuditLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}

	l := &AuditLog{path: path, maxBytes: maxBytes, keep: keep}
	if err := l.open(); err != nil {
		return nil, err
	}

	return l, nil
}

func (l *AuditLog) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return fmt.Errorf("stat audit log: %w", err)
	}

	l.f, l.size = f, info.Size()

	return nil
}

// Write appends e as one line. It is safe for concurrent use.
func (l *AuditLog) Write(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.f == nil {
		return errors.New("audit log closed")
	}

	if l.size > 0 && l.size+int64(len(line)) > l.maxBytes {
		if err := l.rotate(); err != nil {
			return err
		}
	}

	n, err := l.f.Write(line)
	l.size += int64(n)

	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}

	return nil
}

func (l *AuditLog) rotate() error {
	if err := l.f.Close(); err != nil {
		l.f = nil

		return fmt.Errorf("close audit log: %w", err)
	}

	l.f = nil

	var rotErr error

	if l.keep < 1 {
		rotErr = os.Remove(l.path)
	} else {
		rotErr = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep))
		if errors.Is(rotErr, os.ErrNotExist) {
			rotErr = nil
		}

		for n := l.keep; rotErr == nil && n >= 1; n-- {
			src := l.path
			if n > 1 {
				src = fmt.Sprintf("%s.%d", l.path, n-1)
			}

			if err := os.Rename(src, fmt.Sprintf("%s.%d", l.path, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
				rotErr = err
			}
		}
	}

	// Reopen even after a failed rotation so later writes still land.
	if err := l.open(); err != nil {
		return err
	}

	if rotErr != nil {
		return fmt.Errorf("rotate audit log: %w", rotErr)
	}

	return nil
}

// Close closes the underlying file.
func (l *AuditLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.f == nil {
		return nil
	}

	err := l.f.Close()
	l.f = nil

	return err
}
