package deviation_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog/log"
)

// TestMain routes everything the server logs through serverLogs, so a test can
// read what was said, as internal/app's does. Installed here, before any test
// has started a goroutine: replacing the logger while something is logging is
// a data race.
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

// captureLogs keeps what the server logs from here to the end of the test.
// One test at a time: the tests of this package do not run in parallel.
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

// said is the lines kept that carry a fragment of a message, each as the
// fields it was logged with.
func (l *logTap) said(fragment string) []map[string]any {
	l.mu.Lock()
	kept := bytes.Clone(l.kept.Bytes())
	l.mu.Unlock()

	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(kept))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := map[string]any{}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if message, _ := line["message"].(string); strings.Contains(message, fragment) {
			lines = append(lines, line)
		}
	}
	return lines
}
