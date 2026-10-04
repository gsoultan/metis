package impl

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

// decisionScan is one reading of a definition for the places that decide from
// what a step would have set (decisionPointsReading): what is being asked,
// and what has been read once so that it is not read again.
//
// What a point reads is held as a DecisionPoint with no node yet: how many
// names it reads, which of them the step declares, what of those the waiver
// supplies and what it does not. Many steps may read the same thing, so the
// two that can be large — a decision, and everything a call activity hands
// over — are worked out once and shared.
type decisionScan struct {
	// waived is the step being waived, whose own repeating is not asked
	// again — when ownRepeatLeftOut says so.
	waived string
	// ownRepeatLeftOut is false when more than one node carries the waived
	// step's id: which of them the instance is at cannot be told from an id,
	// so the list and the completion condition of each are asked for.
	ownRepeatLeftOut bool
	// declared is the fields the step's form declares; outputs is what the
	// waiver supplies.
	declared map[string]struct{}
	outputs  map[string]any
	// lookup says what a decision reads. It may be nil: no decision is known.
	lookup decisionReads

	// decisions is what each decision consulted reads, by key and version,
	// looked up the first time a step consults it: the lookup is asked once
	// for each, however many steps consult it.
	decisions map[[2]string]entities.DecisionPoint
	// everything is what a call activity with no input mapping is handed:
	// every field the step declares. Worked out the first time one is met.
	everything *entities.DecisionPoint
}

// pointsAt is the decision points the nodes of one id are, at most one of
// each kind, reading what any of the nodes reads. flows are the flows that
// leave the id. A point that reads nothing the step declares is left out,
// unless what it reads could not be told.
func (s *decisionScan) pointsAt(copies []*entities.Node, flows []*entities.SequenceFlow) []entities.DecisionPoint {
	var points []entities.DecisionPoint
	add := func(point entities.DecisionPoint, first *entities.Node) {
		if first == nil || (point.Analysed && len(point.Supplied)+len(point.Missing) == 0) {
			return
		}
		point.NodeID, point.NodeName = first.ID, cmp.Or(first.Name, first.ID)
		points = append(points, point)
	}
	add(s.gateway(copies, flows))
	add(s.readByEach(entities.DecisionPointConditionalEvent, copies, s.conditionWaitedFor))
	add(s.readByEach(entities.DecisionPointCompletionCondition, copies, s.completionCondition))
	add(s.readByEach(entities.DecisionPointCollection, copies, s.collection))
	add(s.decisionTable(copies))
	add(s.calledProcess(copies))
	return points
}

// reading is what a point that reads names takes from the step: how many
// names it reads in all, and those of them the step declares — sorted, each
// once — told apart by whether the waiver supplies them. A value the instance
// already holds does not count.
//
// Only the step's own fields are kept as names. They are what somebody
// waiving the step can act on, and there are no more of them than the form
// has fields, whatever a decision table of thousands of columns reads beside
// them.
func (s *decisionScan) reading(kind entities.DecisionPointKind, names []string, analysed bool) entities.DecisionPoint {
	all := unionOfNames(names)
	point := entities.DecisionPoint{Kind: kind, ReadsInAll: len(all), Analysed: analysed}
	for _, name := range all {
		if _, declared := s.declared[name]; !declared {
			continue
		}
		point.Reads = append(point.Reads, name)
		if _, given := s.outputs[name]; given {
			point.Supplied = append(point.Supplied, name)
		} else {
			point.Missing = append(point.Missing, name)
		}
	}
	point.Reads, point.Supplied, point.Missing = slices.Clip(point.Reads), slices.Clip(point.Supplied), slices.Clip(point.Missing)
	return point
}

// conditionReads is what a condition the evaluator chain is given reads: the
// names in it (logic.ReferencedNames), and its whole text when the step
// declares a field of exactly that name — the chain looks the text up as a
// variable before it parses it (SimpleVariableEvaluator), and a form field may
// be named anything. An empty condition is never looked up: the chain answers
// it before it reads anything.
func (s *decisionScan) conditionReads(condition string) (names []string, analysable bool) {
	names, analysable = logic.ReferencedNames(condition)
	if _, isField := s.declared[condition]; isField && condition != "" {
		names = append(names, condition)
	}
	return names, analysable
}

// gateway is an exclusive or inclusive gateway choosing among its flows: the
// only two steps whose flows' conditions the engine evaluates. The flows are
// read once for the id, however many nodes carry it.
func (s *decisionScan) gateway(copies []*entities.Node, flows []*entities.SequenceFlow) (entities.DecisionPoint, *entities.Node) {
	isGateway := func(node *entities.Node) bool {
		return node.Type == entities.ExclusiveGateway || node.Type == entities.InclusiveGateway
	}
	at := slices.IndexFunc(copies, isGateway)
	if at < 0 {
		return entities.DecisionPoint{}, nil
	}

	var names []string
	analysed := true
	own := make(map[string]struct{}, len(flows))
	for _, flow := range flows {
		read, analysable := s.conditionReads(flow.Condition)
		names = append(names, read...)
		analysed = analysed && analysable
		own[flow.ID] = struct{}{}
	}
	point := s.reading(entities.DecisionPointGateway, names, analysed)

	// A default the gateway names and does not have is no default: the engine
	// raises an incident.
	point.HasDefaultFlow = true
	for _, node := range copies[at:] {
		if _, has := own[node.DefaultFlow]; isGateway(node) && (!has || node.DefaultFlow == "") {
			point.HasDefaultFlow = false
		}
	}
	return point, copies[at]
}

// readByEach is a point of a kind whose every node says for itself what it
// reads: the names of all of them together, analysed only if each is. The
// node returned is the first that is of the kind, nil when none is.
func (s *decisionScan) readByEach(kind entities.DecisionPointKind, copies []*entities.Node,
	read func(*entities.Node) (names []string, analysable, is bool)) (entities.DecisionPoint, *entities.Node) {
	var first *entities.Node
	var names []string
	analysed := true
	for _, node := range copies {
		own, analysable, is := read(node)
		if !is {
			continue
		}
		first = cmp.Or(first, node)
		names = append(names, own...)
		analysed = analysed && analysable
	}
	if first == nil {
		return entities.DecisionPoint{}, nil
	}
	return s.reading(kind, names, analysed), first
}

// conditionWaitedFor is the condition a step waits for. Any step: the engine
// re-reads a condition_expression wherever a token rests on one
// (waitingConditionalNodes), not only on a catch event.
func (s *decisionScan) conditionWaitedFor(node *entities.Node) (names []string, analysable, is bool) {
	condition := node.GetStringProperty("condition_expression")
	if condition == "" {
		return nil, false, false
	}
	names, analysable = s.conditionReads(condition)
	return names, analysable, true
}

// completionCondition is a repeating step or an ad-hoc sub-process deciding
// whether it is done. On any other step the condition is never read, and the
// waived step's own is not asked again for this visit.
func (s *decisionScan) completionCondition(node *entities.Node) (names []string, analysable, is bool) {
	if s.isTheWaivedStep(node) || node.CompletionCondition == "" || (!node.Repeats() && !node.IsAdHoc) {
		return nil, false, false
	}
	names, analysable = s.conditionReads(node.CompletionCondition)
	return names, analysable, true
}

// collection is a repeating step taking its list from a variable, which it
// looks up by exactly that name. Without the list it runs once, with no error
// (NodeHandlerTemplate.handleMultiInstance). The waived step's own list is not
// asked again for this visit.
func (s *decisionScan) collection(node *entities.Node) (names []string, analysable, is bool) {
	if s.isTheWaivedStep(node) || !node.Repeats() || node.Collection == "" {
		return nil, false, false
	}
	return []string{node.Collection}, true, true
}

// isTheWaivedStep reports whether node is the step being waived, and nothing
// else is: only then is its own repeating not asked again.
func (s *decisionScan) isTheWaivedStep(node *entities.Node) bool {
	return s.ownRepeatLeftOut && node.ID == s.waived
}

// decisionTable is a step that consults a decision table, read as the engine
// evaluates it.
//
// A business rule task with an input mapping evaluates its table with the
// mapping's targets, so what the table reads of the process is the mapping's
// sources, whatever the table's columns are called. Otherwise the table sees
// every variable, and reads what the lookup says the consulted version reads.
//
// One step consulting one decision is the usual case, and its point is the
// decision's reading itself, shared with every other step that consults it.
// Only nodes of one id that consult different things are put together.
func (s *decisionScan) decisionTable(copies []*entities.Node) (entities.DecisionPoint, *entities.Node) {
	var first *entities.Node
	var names []string
	var tables []entities.DecisionPoint
	var consulted map[[2]string]struct{}
	analysed := true
	for _, node := range copies {
		key, version, mapping := decisionConsulted(node)
		if key == "" {
			continue
		}
		first = cmp.Or(first, node)
		if len(mapping) > 0 {
			fed, analysable := logic.MappingSourceNames(mapping)
			names = append(names, fed...)
			analysed = analysed && analysable
			continue
		}
		ref := [2]string{key, strconv.Itoa(version)}
		if _, again := consulted[ref]; !again {
			if consulted == nil {
				consulted = map[[2]string]struct{}{}
			}
			consulted[ref] = struct{}{}
			tables = append(tables, s.decision(ref, key, version))
		}
	}
	switch {
	case first == nil:
		return entities.DecisionPoint{}, nil
	case len(tables) == 1 && len(names) == 0 && analysed:
		return tables[0], first
	}
	// Several things read by the nodes of one id. Each decision's reading is
	// already cut down to the step's fields, so putting them together costs
	// what the form is long, not what the tables are. The count is each
	// part's added up: a name two of them read is counted for both, which is
	// never too few.
	inAll := len(unionOfNames(names))
	for _, table := range tables {
		names = append(names, table.Reads...)
		inAll += table.ReadsInAll
		analysed = analysed && table.Analysed
	}
	point := s.reading(entities.DecisionPointDecisionTable, names, analysed)
	point.ReadsInAll = inAll
	return point, first
}

// decision is what one version of a decision reads, asked of the lookup the
// first time and remembered. A decision nobody can find, or one that could
// not be read, is not analysed; with no lookup none can be found.
func (s *decisionScan) decision(ref [2]string, key string, version int) entities.DecisionPoint {
	if known, isKnown := s.decisions[ref]; isKnown {
		return known
	}
	var names []string
	analysed := false
	if s.lookup != nil {
		var analysable, found bool
		names, analysable, found = s.lookup(key, version)
		analysed = analysable && found
	}
	read := s.reading(entities.DecisionPointDecisionTable, names, analysed)
	if s.decisions == nil {
		s.decisions = map[[2]string]entities.DecisionPoint{}
	}
	s.decisions[ref] = read
	return read
}

// decisionConsulted is the decision a step consults, the version it pins (0
// for the one in force) and the mapping its table is fed through, as the two
// handlers that consult one read them: BusinessRuleTaskHandler and
// UserTaskHandler.resolveAssignment. No other step consults a decision,
// whatever its properties say.
func decisionConsulted(node *entities.Node) (key string, version int, mapping map[string]any) {
	switch node.Type {
	case entities.BusinessRuleTask:
		if fed, isMapping := node.Properties[inputMappingProperty].(map[string]any); isMapping {
			mapping = fed
		}
		return node.GetStringProperty("decision_key"), pinnedVersion(node.Properties["decision_version"]), mapping
	case entities.UserTask:
		return strings.TrimSpace(node.GetStringProperty(handlerAssignmentDecisionKey)),
			pinnedVersion(node.Properties[assignmentDecisionVersionProperty]), nil
	}
	return "", 0, nil
}

// pinnedVersion reads a version from a step's properties as both handlers do:
// a number is a version, anything else — text included — is none. Zero or
// less is the version in force, as the decision service reads it.
func pinnedVersion(raw any) int {
	switch v := raw.(type) {
	case float64:
		return max(int(v), 0)
	case int:
		return max(v, 0)
	}
	return 0
}

// calledProcess is a call activity that hands the process it calls a field
// the step declares: every variable when it has no input mapping, and
// otherwise the variables its mapping names (CallActivityHandler looks each
// source up by exactly that name). What the called process does with them is
// not read, so the point is never analysed and nothing is said to be missing
// from it. A call activity handed none of the fields is no point at all.
func (s *decisionScan) calledProcess(copies []*entities.Node) (entities.DecisionPoint, *entities.Node) {
	var first *entities.Node
	var names []string
	everything := false
	for _, node := range copies {
		if node.Type != entities.CallActivity {
			continue
		}
		first = cmp.Or(first, node)
		mapping, isMapping := node.Properties["in_mapping"].(map[string]any)
		if !isMapping || len(mapping) == 0 {
			everything = true
			continue
		}
		for _, source := range mapping {
			name, isText := source.(string)
			if _, isField := s.declared[name]; isText && isField {
				names = append(names, name)
			}
		}
	}
	if first == nil {
		return entities.DecisionPoint{}, nil
	}
	var point entities.DecisionPoint
	if everything {
		point = s.handedEverything()
	} else {
		point = s.reading(entities.DecisionPointCalledProcess, names, false)
	}
	if len(point.Reads) == 0 {
		return entities.DecisionPoint{}, nil
	}
	// Handed over, not known to be read: nothing is known to be missing.
	point.Missing = nil
	return point, first
}

// handedEverything is what a call activity with no input mapping is handed of
// the step's: every field the step declares. Worked out once, however many
// call activities there are.
func (s *decisionScan) handedEverything() entities.DecisionPoint {
	if s.everything == nil {
		all := s.reading(entities.DecisionPointCalledProcess, sortedKeys(s.declared), false)
		s.everything = &all
	}
	return *s.everything
}
