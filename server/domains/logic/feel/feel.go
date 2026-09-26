package feel

import (
	"fmt"
	"sync"

	"github.com/gsoultan/metis/internal/pkg/lru"
)

// Evaluate parses and evaluates an expression against variables.
func Evaluate(expression string, vars map[string]any) (Value, error) {
	node, err := expressionCache.parse(expression, Parse)
	if err != nil {
		return Null, err
	}
	return Eval(node, NewScope(vars))
}

// EvaluateUnaryTests evaluates a DMN decision-table cell against the case in
// front of it. An empty cell, or `-`, matches anything.
//
// input is the value of the cell's own column: what every test compares, and
// what `_input` names. vars are the variables the decision was evaluated with,
// every one in scope by name, so `< maximum` compares with another column and
// `> credit_limit` with a variable no column reads. columns are the variables
// the table's condition columns read; they decide what a lone word means (see
// evalUnaryTest).
func EvaluateUnaryTests(cell string, input any, vars map[string]any, columns []string) (bool, error) {
	node, err := unaryTestCache.parse(cell, ParseUnaryTests)
	if err != nil {
		return false, err
	}

	// Only the input is converted up front; a variable is converted when the
	// cell reads it. The input is in scope before any variable is looked at,
	// so it is what `_input` names whatever the variables hold.
	e := &evaluator{scope: Scope{InputName: FromAny(input)}, unconverted: vars, columns: columns}
	result, err := e.eval(node)
	if err != nil {
		return false, err
	}
	return result.Truthy(), nil
}

// The two grammars get separate caches rather than a shared one with prefixed
// keys: a cell and an expression can be the same text and mean different
// things, and keeping them apart makes that impossible to get wrong.
var (
	expressionCache = &astCache{}
	unaryTestCache  = &astCache{}
)

// astCache remembers parsed expressions.
//
// A decision table evaluates the same cells once per rule per evaluation, and
// gateway conditions sit on the engine's hot path, so the same handful of
// strings are parsed repeatedly. Parsing is pure and evaluation never mutates
// the tree, so a parsed AST is safe to share across goroutines.
type astCache struct {
	once    sync.Once
	entries *lru.Cache[string, cacheEntry]
}

func (c *astCache) cache() *lru.Cache[string, cacheEntry] {
	c.once.Do(func() { c.entries = lru.New[string, cacheEntry](maxCachedExpressions) })
	return c.entries
}

type cacheEntry struct {
	node Node
	err  error
}

// maxCachedExpressions bounds the cache.
//
// Expressions come from deployed definitions, so the set is normally small and
// stable — but "normally" is not a guarantee when the input is untrusted, and
// an unbounded map keyed by remote input is how a service becomes a memory
// leak.
//
// The bound used to be enforced by refusing to grow: past 4096 entries nothing
// new was ever cached again, so one tenant deploying that many distinct
// conditions turned parsing back on for the whole installation, permanently and
// silently. It evicts the least recently used entry instead.
const maxCachedExpressions = 4096

func (c *astCache) parse(text string, parse func(string) (Node, error)) (Node, error) {
	cache := c.cache()
	if entry, ok := cache.Get(text); ok {
		return entry.node, entry.err
	}

	node, err := parse(text)

	// Failures are cached too: a broken expression in a deployed definition is
	// re-evaluated on every instance, and re-parsing it each time to reach the
	// same error is pure waste.
	cache.Put(text, cacheEntry{node: node, err: err})

	return node, err
}

// EvaluateCondition evaluates a gateway or completion condition.
//
// It differs from Evaluate in one way: in an equality comparison, a bare word
// that names no variable is read as text. That is what lets `status = approved`
// mean what its author meant. Decision cells have the same leniency, narrowed
// to the table's own columns (see evalUnaryTest). Conditions in deployed
// definitions are written that way —
// the legacy evaluator compared the right-hand side as a literal — so reading
// it as an unresolvable variable would turn working gateways into dead ones.
//
// The result must be a boolean. Anything else is an error rather than a
// coincidence: a condition that evaluates to a number has not decided anything,
// and routing a token on it would be a guess.
func EvaluateCondition(expression string, vars map[string]any) (bool, error) {
	node, err := conditionCache.parse(expression, Parse)
	if err != nil {
		return false, err
	}

	e := &evaluator{scope: NewScope(vars), lenientEquality: true}
	result, err := e.eval(node)
	if err != nil {
		return false, err
	}
	if result.Kind != KindBoolean {
		return false, fmt.Errorf("feel: a condition must be true or false, but %q gives a %s",
			expression, result.Kind)
	}
	return result.Bool, nil
}

// conditionCache is separate from expressionCache because the same text
// evaluates differently under lenient equality.
var conditionCache = &astCache{}
