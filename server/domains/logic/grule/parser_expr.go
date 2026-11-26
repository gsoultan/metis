package grule

import (
	"strconv"
	"strings"
)

// binaryLevels are GRL's infix operators from the loosest binding to the
// tightest. GRL follows Go's precedence.
var binaryLevels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", "<=", ">", ">="},
	{"+", "-", "|"},
	{"*", "/", "%", "&"},
}

// ParseExpression parses one GRL expression on its own: the form Grule's JSON
// rules use for a condition or an operand written as text.
func ParseExpression(source string) (Expr, error) {
	tokens, err := lex(source)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.at(tokEOF, "") {
		return nil, p.errorf("unexpected %s after the expression", describe(p.peek()))
	}
	return expr, nil
}

func (p *parser) parseExpr() (Expr, error) { return p.parseLevel(0) }

func (p *parser) enter() error {
	p.depth++
	if p.depth > MaxDepth {
		return p.errorf("an expression nests more than %d levels deep", MaxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) parseLevel(level int) (Expr, error) {
	if level == len(binaryLevels) {
		return p.parseUnary()
	}
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	left, err := p.parseLevel(level + 1)
	if err != nil {
		return nil, err
	}
	// `a + a + a …` is built to the left in this loop rather than by
	// recursion, so each operator is counted as a level here: the tree is as
	// deep as the chain is long, and everything that walks it recurses.
	chain := 0
	defer func() { p.depth -= chain }()
	for {
		op, ok := p.binaryOp(binaryLevels[level])
		if !ok {
			return left, nil
		}
		chain++
		if err := p.enter(); err != nil {
			return nil, err
		}
		right, err := p.parseLevel(level + 1)
		if err != nil {
			return nil, err
		}
		left = Binary{Op: op, Left: left, Right: right}
	}
}

func (p *parser) binaryOp(ops []string) (string, bool) {
	t := p.peek()
	if t.kind != tokPunct {
		return "", false
	}
	for _, op := range ops {
		if t.text == op {
			p.advance()
			return op, true
		}
	}
	return "", false
}

func (p *parser) parseUnary() (Expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	for _, op := range []string{"!", "-"} {
		if p.accept(tokPunct, op) {
			operand, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return Unary{Op: op, Operand: operand}, nil
		}
	}
	primary, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return p.parsePostfix(primary)
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch {
	case t.kind == tokString:
		p.advance()
		return Literal{Value: t.text}, nil
	case t.kind == tokNumber:
		p.advance()
		value, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, p.errorf("%q is not a number", t.text)
		}
		return Literal{Value: value}, nil
	case p.accept(tokPunct, "("):
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokPunct, ")", "`)`"); err != nil {
			return nil, err
		}
		return inner, nil
	case t.kind == tokIdent && !isKeyword(t.text):
		return p.parseName()
	}
	return nil, p.errorf("expected a value, found %s", describe(t))
}

// isKeyword reports whether a word is one GRL reserves for a rule's shape,
// which an expression cannot use as a name.
func isKeyword(word string) bool {
	switch strings.ToLower(word) {
	case "rule", "salience", "when", "then":
		return true
	}
	return false
}

// parseName reads a literal word, a function call, or the start of a path.
func (p *parser) parseName() (Expr, error) {
	word := p.advance().text
	switch strings.ToLower(word) {
	case "true":
		return Literal{Value: true}, nil
	case "false":
		return Literal{Value: false}, nil
	case "nil":
		return Literal{Value: nil}, nil
	}
	if p.at(tokPunct, "(") {
		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		return Call{Name: word, Args: args}, nil
	}
	return Path{Parts: []string{word}}, nil
}

// parsePostfix reads the `.field`, `.Method(...)` and `[key]` after a value.
func (p *parser) parsePostfix(expr Expr) (Expr, error) {
	chain := 0
	defer func() { p.depth -= chain }()
	for {
		chain++
		if err := p.enter(); err != nil {
			return nil, err
		}
		switch {
		case p.accept(tokPunct, "."):
			name, err := p.expect(tokIdent, "", "a name after `.`")
			if err != nil {
				return nil, err
			}
			if expr, err = p.member(expr, name.text); err != nil {
				return nil, err
			}
		case p.accept(tokPunct, "["):
			key, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tokPunct, "]", "`]`"); err != nil {
				return nil, err
			}
			expr = Index{Target: expr, Key: key}
		default:
			return expr, nil
		}
	}
}

func (p *parser) member(receiver Expr, name string) (Expr, error) {
	if p.at(tokPunct, "(") {
		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		return Call{Receiver: receiver, Name: name, Args: args}, nil
	}
	if path, isPath := receiver.(Path); isPath {
		parts := append(append([]string(nil), path.Parts...), name)
		return Path{Parts: parts}, nil
	}
	return nil, p.errorf("a field of a computed value (`.%s`) is not supported", name)
}

func (p *parser) parseArgs() ([]Expr, error) {
	p.advance() // (
	var args []Expr
	for !p.accept(tokPunct, ")") {
		if len(args) > 0 {
			if _, err := p.expect(tokPunct, ",", "`,` or `)`"); err != nil {
				return nil, err
			}
		}
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}
