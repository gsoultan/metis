package grule

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseGRL parses a GRL rule file. name is the file's name, used only in
// positions and messages.
func ParseGRL(name, source string) ([]Rule, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("%s is %d bytes; a rule file may be at most %d", name, len(source), MaxSourceBytes)
	}
	tokens, err := lex(source)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	p := &parser{file: name, tokens: tokens}
	var rules []Rule
	for !p.at(tokEOF, "") {
		if len(rules) == MaxRules {
			return nil, fmt.Errorf("%s holds more than %d rules", name, MaxRules)
		}
		rule, err := p.parseRule()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

type parser struct {
	file   string
	tokens []token
	pos    int
	depth  int
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) advance() token {
	t := p.tokens[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

// at reports whether the next token is of kind and, when text is set, says
// text. Words are compared without regard to case, as GRL's keywords are.
func (p *parser) at(kind tokenKind, text string) bool {
	t := p.peek()
	if t.kind != kind {
		return false
	}
	return text == "" || t.text == text || (kind == tokIdent && strings.EqualFold(t.text, text))
}

func (p *parser) accept(kind tokenKind, text string) bool {
	if p.at(kind, text) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) expect(kind tokenKind, text, what string) (token, error) {
	if !p.at(kind, text) {
		return token{}, p.errorf("expected %s, found %s", what, describe(p.peek()))
	}
	return p.advance(), nil
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", p.peek().line, fmt.Sprintf(format, args...))
}

func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "the end of the file"
	case tokString:
		return strconv.Quote(t.text)
	}
	return "`" + t.text + "`"
}

// parseRule reads `rule Name "description" salience N { when ... then ... }`.
func (p *parser) parseRule() (Rule, error) {
	start, err := p.expect(tokIdent, "rule", "`rule`")
	if err != nil {
		return Rule{}, err
	}
	name, err := p.expect(tokIdent, "", "the rule's name")
	if err != nil {
		return Rule{}, err
	}
	rule := Rule{Name: name.text, Position: fmt.Sprintf("%s:%d", p.file, start.line)}
	if p.at(tokString, "") {
		rule.Description = p.advance().text
	}
	if p.accept(tokIdent, "salience") {
		if rule.Salience, err = p.parseSalience(); err != nil {
			return Rule{}, err
		}
	}
	if _, err := p.expect(tokPunct, "{", "`{`"); err != nil {
		return Rule{}, err
	}
	if err := p.parseBody(&rule); err != nil {
		return Rule{}, fmt.Errorf("rule %s: %w", rule.Name, err)
	}
	return rule, nil
}

func (p *parser) parseSalience() (int, error) {
	negative := p.accept(tokPunct, "-")
	number, err := p.expect(tokNumber, "", "a whole number after `salience`")
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(number.text)
	if err != nil {
		return 0, p.errorf("salience %q is not a whole number", number.text)
	}
	if negative {
		value = -value
	}
	return value, nil
}

func (p *parser) parseBody(rule *Rule) error {
	if _, err := p.expect(tokIdent, "when", "`when`"); err != nil {
		return err
	}
	when, err := p.parseExpr()
	if err != nil {
		return err
	}
	rule.When = when
	if _, err := p.expect(tokIdent, "then", "`then`"); err != nil {
		return err
	}
	for !p.accept(tokPunct, "}") {
		if len(rule.Then) == MaxStatements {
			return p.errorf("more than %d actions in one rule", MaxStatements)
		}
		statement, err := p.parseStatement()
		if err != nil {
			return err
		}
		rule.Then = append(rule.Then, statement)
	}
	return nil
}

// parseStatement reads one action. The `;` after it may be left off before the
// rule's closing brace.
func (p *parser) parseStatement() (Statement, error) {
	target, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	var statement Statement
	if op := p.peek(); op.kind == tokPunct && isAssignOp(op.text) {
		p.advance()
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		statement = Assign{Target: target, Op: op.text, Value: value}
	} else if call, isCall := target.(Call); isCall {
		statement = CallStatement{Call: call}
	} else {
		return nil, p.errorf("an action must assign a value or call a function")
	}
	if !p.accept(tokPunct, ";") && !p.at(tokPunct, "}") {
		return nil, p.errorf("expected `;` after an action, found %s", describe(p.peek()))
	}
	return statement, nil
}

func isAssignOp(op string) bool {
	return op == "=" || op == "+=" || op == "-=" || op == "*=" || op == "/="
}
