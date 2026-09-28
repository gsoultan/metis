package task_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/rs/zerolog/log"
)

// TestMain routes everything the server logs through serverLogs, so a test can
// read what was said. Installed here, before any test has started a goroutine:
// replacing the logger while something is logging is a data race.
func TestMain(m *testing.M) {
	log.Logger = log.Logger.Output(serverLogs)
	os.Exit(m.Run())
}

// serverLogs passes every line on to stderr, and keeps a copy while a test
// asks for one.
var serverLogs = &logTap{}

type logTap struct {
	mu   sync.Mutex
	kept *bytes.Buffer
}

func (l *logTap) Write(line []byte) (int, error) {
	l.mu.Lock()
	if l.kept != nil {
		l.kept.Write(line)
	}
	l.mu.Unlock()
	return os.Stderr.Write(line)
}

// captureLogs keeps what is logged from here to the end of the test.
func captureLogs(t *testing.T) *logTap {
	t.Helper()
	serverLogs.mu.Lock()
	serverLogs.kept = &bytes.Buffer{}
	serverLogs.mu.Unlock()
	t.Cleanup(func() {
		serverLogs.mu.Lock()
		serverLogs.kept = nil
		serverLogs.mu.Unlock()
	})
	return serverLogs
}

// linesNaming returns each kept line that names the setting, decoded.
func (l *logTap) linesNaming(setting string) []map[string]any {
	l.mu.Lock()
	kept := bytes.Clone(l.kept.Bytes())
	l.mu.Unlock()

	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(kept))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line map[string]any
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line["setting"] == setting {
			lines = append(lines, line)
		}
	}
	return lines
}
