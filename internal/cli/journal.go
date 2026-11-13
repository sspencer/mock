package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"

	"github.com/sspencer/mock/mockhttp"
)

// journal appends every request event to a file as one JSON line
// (--journal), so the traffic outlives the in-memory log.
type journal struct {
	mu     sync.Mutex
	file   *os.File
	logger *slog.Logger
	failed bool
}

// openJournal opens path for appending, creating it if needed.
func openJournal(path string, logger *slog.Logger) (*journal, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &journal{file: file, logger: logger}, nil
}

// write is the server's journal callback.
func (j *journal) write(event mockhttp.RequestEvent) {
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	line = append(line, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.file.Write(line); err != nil && !j.failed {
		// Warn once; a full disk would otherwise log on every request.
		j.failed = true
		j.logger.Warn("cannot write the journal", "file", j.file.Name(), "error", err)
	}
}

func (j *journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.file.Close()
}
