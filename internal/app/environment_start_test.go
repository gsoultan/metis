package app

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Creating an environment used to record a row and nothing else: its port was
// bound, its database opened and its workers started at the next restart of
// every replica. An administrator who added staging and pointed a team at it
// found nothing listening.
func TestAnEnvironmentCreatedWhileTheServerRunsIsServed(t *testing.T) {
	fastEnvironmentChecks(t)
	h := newEnvironmentHarness(t)
	h.serve(noContent)

	staging := h.environment("staging", scratchDatabase(t))

	within(t, startLimit, "the environment created while the server was running was never served on its port", func() bool {
		return answers(t, staging.Port, "/")
	})
}

// Disabling an environment stops it being served; enabling it again did not
// start it until a restart.
func TestAReEnabledEnvironmentIsServedAgain(t *testing.T) {
	fastEnvironmentChecks(t)
	h := newEnvironmentHarness(t)
	staging := h.environment("staging", scratchDatabase(t))
	h.serve(noContent)
	within(t, startLimit, "the environment was never served", func() bool { return answers(t, staging.Port, "/") })

	staging.Enabled = false
	h.save(staging)
	within(t, startLimit, "the disabled environment's port still answers", func() bool { return refuses(t, staging.Port) })

	staging.Enabled = true
	h.save(staging)
	within(t, startLimit, "the re-enabled environment was never served again", func() bool {
		return answers(t, staging.Port, "/")
	})
}

// Pointing an environment at another database changed its row and nothing
// else: its port went on reading and writing the old database until a
// restart, while the settings page showed the new one.
func TestARePointedEnvironmentServesItsNewDatabase(t *testing.T) {
	fastEnvironmentChecks(t)
	h := newEnvironmentHarness(t)
	before, after := scratchDatabase(t), scratchDatabase(t)
	beforeDB, afterDB := behindTheServer(t, before), behindTheServer(t, after)
	for _, database := range []*gorm.DB{beforeDB, afterDB} {
		if err := database.Exec("CREATE TABLE written_here (note text)").Error; err != nil {
			t.Fatalf("prepare a database: %v", err)
		}
	}
	staging := h.environment("staging", before)
	h.serve(writesTheNote(h))

	within(t, startLimit, "the environment was never served", func() bool {
		return answers(t, staging.Port, "/?note=first")
	})
	if n := countNotes(t, beforeDB, "first"); n != 1 {
		t.Fatalf("a note written through the port is in its database %d times, want 1", n)
	}

	staging.Connection["db_name"] = after
	h.save(staging)

	within(t, startLimit, "after the environment was pointed at another database, what is written through its port still lands in the old one", func() bool {
		note := uuid.NewString()
		return answers(t, staging.Port, "/?note="+note) && countNotes(t, afterDB, note) == 1
	})
}

// An environment that cannot start — its database is not there, its port is
// taken — must not hold up the others, must be tried again rather than given
// up on, and must say why once rather than at every check.
func TestAnEnvironmentThatCannotStartIsRetriedWithoutHoldingUpTheOthers(t *testing.T) {
	fastEnvironmentChecks(t)
	logs := captureLogs(t)
	h := newEnvironmentHarness(t)
	good := h.environment("good", scratchDatabase(t))
	missing := unusedDatabaseName()
	noDatabase := h.environment("no-database", missing)
	busy := h.environment("busy-port", scratchDatabase(t))
	release := holdPort(t, busy.Port)
	h.serve(noContent)

	within(t, startLimit, "an environment that could start was held up by the ones that could not", func() bool {
		return answers(t, good.Port, "/")
	})
	within(t, startLimit, "an environment that could not start was not tried again", func() bool {
		return attempts(logs, "no-database") >= 3 && attempts(logs, "busy-port") >= 3
	})
	for _, name := range []string{"no-database", "busy-port"} {
		lines := logs.lines(name, notStarted)
		if lines["error"] != 1 {
			t.Fatalf("over %d attempts, %s's failure was reported %d times; once per cause is what can be read",
				attempts(logs, name), name, lines["error"])
		}
	}

	createDatabase(t, missing)
	within(t, startLimit, "an environment whose database appeared later was never served", func() bool {
		return answers(t, noDatabase.Port, "/")
	})
	release()
	within(t, startLimit, "an environment whose port came free later was never served", func() bool {
		return answers(t, busy.Port, "/")
	})
}

// notStarted is what the watcher says, at every level, about an environment
// it could not start.
const notStarted = "could not be started"

func attempts(logs *logTap, environment string) int {
	total := 0
	for _, n := range logs.lines(environment, notStarted) {
		total += n
	}
	return total
}

// writesTheNote stores ?note= through the executor the request's environment
// binding resolves — the way every repository reaches its database.
func writesTheNote(h *environmentHarness) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executor, err := h.conn.Executor(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if _, err := executor.Exec(r.Context(), "INSERT INTO written_here (note) VALUES ($1)",
			[]any{r.URL.Query().Get("note")}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func countNotes(t *testing.T, database *gorm.DB, note string) int64 {
	t.Helper()
	var n int64
	if err := database.Raw("SELECT count(*) FROM written_here WHERE note = ?", note).Scan(&n).Error; err != nil {
		t.Fatalf("count the notes: %v", err)
	}
	return n
}
