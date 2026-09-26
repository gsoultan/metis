package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/rs/zerolog"
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

// captureLogs keeps what is logged from here to the end of the test, at every
// level: a repeated failure is logged at debug, and counting those is how a
// test knows the watcher really did try again.
func captureLogs(t *testing.T) *logTap {
	t.Helper()
	level := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	serverLogs.mu.Lock()
	serverLogs.kept = &bytes.Buffer{}
	serverLogs.mu.Unlock()
	t.Cleanup(func() {
		serverLogs.mu.Lock()
		serverLogs.kept = nil
		serverLogs.mu.Unlock()
		zerolog.SetGlobalLevel(level)
	})
	return serverLogs
}

// lines returns the kept lines that name the environment and carry the
// message fragment, by level.
func (l *logTap) lines(environment, fragment string) map[string]int {
	l.mu.Lock()
	kept := bytes.Clone(l.kept.Bytes())
	l.mu.Unlock()

	byLevel := map[string]int{}
	scanner := bufio.NewScanner(bytes.NewReader(kept))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			Level       string `json:"level"`
			Environment string `json:"environment"`
			Message     string `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if line.Environment == environment && bytes.Contains([]byte(line.Message), []byte(fragment)) {
			byLevel[line.Level]++
		}
	}
	return byLevel
}
