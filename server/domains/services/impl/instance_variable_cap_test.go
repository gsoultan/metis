package impl

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

const testCap = 4 << 10 // 4 KiB, so the test writes kilobytes and not a megabyte

func blob(n int) map[string]any { return map[string]any{"doc": strings.Repeat("x", n)} }

func instanceWithVariables(t *testing.T, vars map[string]any) (*Engine, repositories.Repository, testutils.Process) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	engine := NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	p := testutils.SeedProcess(t, repo)
	stored, err := repo.Process().Get(p.Tenant, p.Instance)
	if err != nil {
		t.Fatal(err)
	}
	stored.Variables = vars
	if err := repo.Process().Update(p.Tenant, stored); err != nil {
		t.Fatal(err)
	}
	return engine, repo, p
}

func TestAStepThatGrowsVariablesPastTheCapIsRefused(t *testing.T) {
	engine, _, p := instanceWithVariables(t, blob(1<<10))
	err := engine.refuseGrowthPastCap(p.Tenant, p.Instance, blob(8<<10), testCap)
	if !errors.Is(err, ErrInstanceVariablesTooLarge) || !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("growing 1 KiB of variables to 8 KiB past a 4 KiB cap: err %v", err)
	}
	if err := engine.refuseGrowthPastCap(p.Tenant, p.Instance, blob(3<<10), testCap); err != nil {
		t.Fatalf("growth that stays under the cap was refused: %v", err)
	}
}

// An instance already over the cap when it came in is not broken by it: it
// keeps running for as long as it does not grow.
func TestAnInstanceAlreadyOverTheCapKeepsRunningWhileItDoesNotGrow(t *testing.T) {
	engine, _, p := instanceWithVariables(t, blob(10<<10))
	for _, n := range []int{10 << 10, 9 << 10} {
		if err := engine.refuseGrowthPastCap(p.Tenant, p.Instance, blob(n), testCap); err != nil {
			t.Fatalf("an instance over the cap writing %d bytes, not more than it holds, was refused: %v", n, err)
		}
	}
	if err := engine.refuseGrowthPastCap(p.Tenant, p.Instance, blob(11<<10), testCap); !errors.Is(err, ErrInstanceVariablesTooLarge) {
		t.Fatalf("an instance over the cap grew further: err %v", err)
	}
}

func TestAZeroCapIsOff(t *testing.T) {
	engine := &Engine{}
	if err := engine.refuseGrowthPastCap(t.Context(), uuid.New(), blob(1<<20), 0); err != nil {
		t.Fatal(err)
	}
	if err := refuseOversizedStart(blob(1<<20), 0); err != nil {
		t.Fatal(err)
	}
}

func TestAnInstanceCannotStartOverTheCap(t *testing.T) {
	if err := refuseOversizedStart(blob(8<<10), testCap); !errors.Is(err, ErrInstanceVariablesTooLarge) {
		t.Fatalf("err %v", err)
	}
	if err := refuseOversizedStart(blob(1<<10), testCap); err != nil {
		t.Fatal(err)
	}
}
