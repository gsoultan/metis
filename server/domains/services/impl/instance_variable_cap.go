package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/rs/zerolog/log"
)

// A cap on how large an instance's variables may grow.
//
// Nothing bounded them. A loop that appends to a list, or a service task whose
// partner answers with a document, grew one instance's variables on every
// step, and every step wrote all of them again: to the instance's row, and to
// a snapshot of them. An instance is a durable commitment, so a cap that
// broke a running one would be worse than the growth, which is why it refuses
// growth past the cap and not size: an instance already over it when the cap
// came in keeps running for as long as it does not grow.
//
// A step that would grow past the cap fails with ErrInstanceVariablesTooLarge,
// which a job retries and then raises as an incident, and an API call returns
// as an invalid argument.

const (
	envMaxInstanceVariablesBytes = "METIS_MAX_INSTANCE_VARIABLES_BYTES"
	// defaultMaxInstanceVariablesBytes is 1 MiB of JSON.
	defaultMaxInstanceVariablesBytes = 1 << 20
)

// ErrInstanceVariablesTooLarge refuses a write that would take an instance's
// variables past the cap.
var ErrInstanceVariablesTooLarge = errors.New("the instance's variables would grow past " + envMaxInstanceVariablesBytes)

var maxInstanceVariablesBytes = sync.OnceValue(func() int {
	raw := envvar.Get(envMaxInstanceVariablesBytes)
	if raw == "" {
		return defaultMaxInstanceVariablesBytes
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Warn().Str("value", raw).Int("default", defaultMaxInstanceVariablesBytes).
			Msg(envMaxInstanceVariablesBytes + " is not a whole number of bytes; using the default")
		return defaultMaxInstanceVariablesBytes
	}
	return n // zero turns the cap off
})

// variablesSize is how large vars are as JSON, which is how they are stored.
func variablesSize(vars map[string]any) (int, error) {
	if len(vars) == 0 {
		return 0, nil
	}
	encoded, err := json.Marshal(vars)
	if err != nil {
		return 0, fmt.Errorf("could not measure the instance's variables: %w", err)
	}
	return len(encoded), nil
}

// refuseOversizedStart refuses to start an instance whose variables are over
// the cap: it has none stored yet, so there is nothing to grandfather.
func refuseOversizedStart(vars map[string]any, limit int) error {
	if limit <= 0 {
		return nil
	}
	size, err := variablesSize(vars)
	if err != nil {
		return err
	}
	if size > limit {
		return fmt.Errorf("%w: %w (%d bytes, at most %d)", apierr.ErrInvalidArgument, ErrInstanceVariablesTooLarge, size, limit)
	}
	return nil
}

// refuseGrowthPastCap refuses a write of vars to instance id when they are
// over the cap and larger than what is stored. The stored variables are read
// only when the new ones are over it, so an instance under the cap pays one
// encoding per write and no read.
func (e *Engine) refuseGrowthPastCap(ctx context.Context, id uuid.UUID, vars map[string]any, limit int) error {
	if limit <= 0 {
		return nil
	}
	size, err := variablesSize(vars)
	if err != nil {
		return err
	}
	if size <= limit {
		return nil
	}
	stored, err := e.repo.Process().Get(ctx, id)
	if err != nil {
		return fmt.Errorf("could not read the instance to compare its variables' size: %w", err)
	}
	before, err := variablesSize(stored.Variables)
	if err != nil {
		return err
	}
	if size <= before {
		return nil
	}
	return fmt.Errorf("%w: %w (%d bytes, at most %d; %d stored)",
		apierr.ErrInvalidArgument, ErrInstanceVariablesTooLarge, size, limit, before)
}
