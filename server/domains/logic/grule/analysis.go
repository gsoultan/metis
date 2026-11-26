package grule

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/server/domains/logic/feel"
)

// analyse reads one rule's actions and condition. It returns why the rule
// cannot be translated when it cannot.
func analyse(rule Rule, order int) (*analysedRule, string) {
	r := &analysedRule{Rule: rule, order: order, values: map[string]Expr{}, reads: map[string]bool{}}
	for _, statement := range rule.Then {
		if reason := r.addStatement(statement); reason != "" {
			return nil, reason
		}
	}
	if len(r.writes) == 0 {
		return nil, "it sets no field, so a decision has nothing to answer for it"
	}
	for _, condition := range conjuncts(rule.When) {
		pathsIn(condition, r.reads)
		if r.isGuard(condition) {
			r.guarded = true
		}
	}
	for _, key := range r.writes {
		pathsIn(r.values[key], r.reads)
	}
	if !r.guarded && !r.retracts {
		return nil, fmt.Sprintf("it neither calls Retract(%q) nor tests a field it sets, so Grule would fire it on every cycle until its cycle limit", rule.Name)
	}
	if reason := r.prepare(); reason != "" {
		return nil, reason
	}
	if r.divides() {
		r.notes = append(r.notes, "it divides: Grule drops the remainder when both numbers are whole, FEEL keeps it")
	}
	return r, ""
}

// divides reports whether the rule's condition or any of its values divides.
func (r *analysedRule) divides() bool {
	if usesOperator(r.When, "/") {
		return true
	}
	for _, value := range r.values {
		if usesOperator(value, "/") {
			return true
		}
	}
	return false
}

func (r *analysedRule) addStatement(statement Statement) string {
	switch s := statement.(type) {
	case CallStatement:
		return r.addCall(s.Call)
	case Assign:
		return r.addAssign(s)
	}
	return "it has an action of a kind metis does not know"
}

func (r *analysedRule) addCall(call Call) string {
	if call.Receiver != nil {
		return fmt.Sprintf("it calls the method %s(), whose effect a decision cannot reproduce", call.Name)
	}
	switch call.Name {
	case "Retract":
		if len(call.Args) == 1 && namesRule(call.Args[0], r.Name) {
			r.retracts = true
			return ""
		}
		return "it retracts another rule, which changes which rules Grule considers"
	case "Log":
		r.notes = append(r.notes, "its Log() call is dropped")
		return ""
	}
	return fmt.Sprintf("it calls %s(), which a decision cannot do", call.Name)
}

// namesRule reports whether a Retract argument names the rule: `"Name"`, or
// the bare word Name.
func namesRule(arg Expr, name string) bool {
	switch a := arg.(type) {
	case Literal:
		return a.Value == name
	case Path:
		return len(a.Parts) == 1 && a.Parts[0] == name
	}
	return false
}

func (r *analysedRule) addAssign(a Assign) string {
	target, isPath := a.Target.(Path)
	if !isPath {
		return "it sets an element of a list or map, which a decision cannot answer"
	}
	key := fieldKey(target)
	if r.writesField(key) {
		return fmt.Sprintf("it sets %s twice", key)
	}
	value := a.Value
	if a.Op != "=" {
		value = Binary{Op: strings.TrimSuffix(a.Op, "="), Left: target, Right: a.Value}
	}
	r.values[key] = substitute(value, r.values)
	r.writes = append(r.writes, key)
	return ""
}

// isGuard reports whether a condition is one the rule's own action makes
// false: `Result == ""` in a rule that sets Result to "approved", `!Done` in
// one that sets Done to true. A rule with a guard fires once on its own.
func (r *analysedRule) isGuard(condition Expr) bool {
	if u, ok := condition.(Unary); ok && u.Op == "!" {
		return r.setsLiteral(u.Operand, func(v any) bool { return v == true })
	}
	b, ok := condition.(Binary)
	if !ok || (b.Op != "==" && b.Op != "!=") {
		return false
	}
	field, literal, ok := pathAndLiteral(b)
	if !ok {
		return false
	}
	if b.Op == "==" {
		return r.setsLiteral(field, func(v any) bool { return v != literal.Value }) || r.setsComputed(field)
	}
	return r.setsLiteral(field, func(v any) bool { return v == literal.Value })
}

// setsComputed reports whether the rule sets the field a path names to a
// value it computes. `Discount == 0` guarding `Discount = Total * 0.2` is
// taken as a guard: if the value ever came out as the constant tested,
// Grule would fire the rule until its cycle limit and fail, so a rule set
// that works in Grule never does that.
func (r *analysedRule) setsComputed(expr Expr) bool {
	path, ok := expr.(Path)
	if !ok {
		return false
	}
	value, set := r.values[fieldKey(path)]
	if !set {
		return false
	}
	_, constant := value.(Literal)
	return !constant
}

// setsLiteral reports whether the rule sets the field expr names to a
// constant that holds is true of.
func (r *analysedRule) setsLiteral(expr Expr, holds func(any) bool) bool {
	path, ok := expr.(Path)
	if !ok {
		return false
	}
	value, ok := r.values[fieldKey(path)].(Literal)
	return ok && holds(value.Value)
}

// pathAndLiteral splits `path op literal`, or `literal op path`.
func pathAndLiteral(b Binary) (Path, Literal, bool) {
	if p, ok := b.Left.(Path); ok {
		if l, ok := b.Right.(Literal); ok {
			return p, l, true
		}
	}
	if p, ok := b.Right.(Path); ok {
		if l, ok := b.Left.(Literal); ok {
			return p, l, true
		}
	}
	return Path{}, Literal{}, false
}

// prepare writes the rule's conditions and values as FEEL, so a rule with an
// expression FEEL has no form for is turned away here, with its reason,
// before anything is built from it.
func (r *analysedRule) prepare() string {
	for _, part := range conjuncts(r.When) {
		if constant, isLiteral := part.(Literal); isLiteral {
			if constant.Value == true {
				continue
			}
			return "its condition can never be true"
		}
		c, err := newCondition(part)
		if err != nil {
			return "its condition: " + err.Error()
		}
		if err := c.check(); err != nil {
			return "its condition could not be written as FEEL: " + err.Error()
		}
		r.conditions = append(r.conditions, c)
	}
	r.feelValues = map[string]string{}
	for _, key := range r.writes {
		if !usableName(key) {
			return fmt.Sprintf("it sets %s, which has no name FEEL can use", key)
		}
		written, err := writeFEEL(r.values[key])
		if err == nil {
			_, err = feel.Parse(written)
		}
		if err != nil {
			return fmt.Sprintf("the value it sets %s to: %s", key, err)
		}
		r.feelValues[key] = written
	}
	return ""
}
