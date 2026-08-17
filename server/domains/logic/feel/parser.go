package feel

import (
	"fmt"
)

// Binding powers for the Pratt parser. Higher binds tighter.
const (
	bpNone    = 0
	bpOr      = 10
	bpAnd     = 20
	bpCompare = 30 // = != < <= > >= between in
	bpSum     = 40 // + -
	bpProduct = 50 // * /
	bpPower   = 60 // **
	bpUnary   = 70 // -x
	bpPostfix = 80 // . [ (
)

// maxDepth bounds nesting. An expression comes from a deployed definition,
// which is untrusted input, and a recursive-descent parser turns deep nesting
// into deep recursion — `not(not(not(…)))` a few thousand levels down would
// exhaust the goroutine stack and take the process with it. The limit is far
// above any expression a person writes and far below the stack.
const maxDepth = 64

type parser struct {
	tokens []token
	pos    int
	depth  int

	// inRangeBound is non-zero while a range's upper bound is being read, which
	// is the one place a `[` closes something instead of opening an index.
	inRangeBound int
}

// Parse turns an expression into an AST.
func Parse(input string) (Node, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}

	node, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	if !p.at(tokEOF) {
		return nil, fmt.Errorf("feel: unexpected %s after the expression", p.peek())
	}
	return node, nil
}

// ParseUnaryTests parses a DMN decision-table cell.
//
// A cell is not an expression: `< 100` has nothing on its left, `"GOLD","SILVER"`
// is a disjunction rather than a list, and `-` means "any value". Parsing it as
// its own grammar is what removes the guesswork the string matcher needed.
func ParseUnaryTests(input string) (Node, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}

	// An empty cell, or `-`, matches anything.
	if p.at(tokEOF) {
		return &UnaryTests{MatchAll: true}, nil
	}
	if p.at(tokOperator) && p.peek().text == "-" && p.tokens[p.pos+1].kind == tokEOF {
		return &UnaryTests{MatchAll: true}, nil
	}

	// `not(...)` inverts the tests inside it.
	negated := false
	if p.at(tokKeyword) && p.peek().text == "not" && p.tokens[p.pos+1].kind == tokLParen {
		negated = true
		p.next() // not
		p.next() // (
	}

	tests, err := p.parseUnaryTestList()
	if err != nil {
		return nil, err
	}

	if negated {
		if !p.at(tokRParen) {
			return nil, fmt.Errorf("feel: not( … is missing its closing parenthesis")
		}
		p.next()
	}
	if !p.at(tokEOF) {
		return nil, fmt.Errorf("feel: unexpected %s after the test", p.peek())
	}
	return &UnaryTests{Tests: tests, Negated: negated}, nil
}

func (p *parser) parseUnaryTestList() ([]Node, error) {
	var tests []Node
	for {
		test, err := p.parseUnaryTest()
		if err != nil {
			return nil, err
		}
		tests = append(tests, test)

		if !p.at(tokComma) {
			return tests, nil
		}
		p.next()
	}
}

func (p *parser) parseUnaryTest() (Node, error) {
	// A leading comparison operator: `< 100`, `>= 0`.
	if p.at(tokOperator) {
		switch op := p.peek().text; op {
		case "<", "<=", ">", ">=", "=", "!=":
			p.next()
			expr, err := p.parseExpr(bpCompare)
			if err != nil {
				return nil, err
			}
			return &UnaryTest{Op: op, Expr: expr}, nil
		}
	}

	// Anything else is an expression the input is compared to for equality —
	// including a range, which compares by containment.
	expr, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	return &UnaryTest{Expr: expr}, nil
}

// parseExpr is the Pratt loop: parse a prefix, then absorb infix operators
// while they bind tighter than the caller's limit.
func (p *parser) parseExpr(minBP int) (Node, error) {
	if p.depth++; p.depth > maxDepth {
		return nil, fmt.Errorf("feel: expression nests deeper than %d levels", maxDepth)
	}
	defer func() { p.depth-- }()

	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}

	for {
		bp := p.infixBindingPower()
		if bp <= minBP {
			return left, nil
		}
		left, err = p.parseInfix(left, bp)
		if err != nil {
			return nil, err
		}
	}
}

func (p *parser) parsePrefix() (Node, error) {
	tok := p.peek()

	switch tok.kind {
	case tokNumber:
		p.next()
		return &Literal{Value: Num(tok.num)}, nil

	case tokString:
		p.next()
		return &Literal{Value: Str(tok.text)}, nil

	case tokKeyword:
		switch tok.text {
		case "true":
			p.next()
			return &Literal{Value: True}, nil
		case "false":
			p.next()
			return &Literal{Value: False}, nil
		case "null":
			p.next()
			return &Literal{Value: Null}, nil
		case "not":
			p.next()
			if !p.at(tokLParen) {
				return nil, fmt.Errorf("feel: not must be written not(…)")
			}
			p.next()
			operand, err := p.parseExpr(bpNone)
			if err != nil {
				return nil, err
			}
			if !p.at(tokRParen) {
				return nil, fmt.Errorf("feel: not( … is missing its closing parenthesis")
			}
			p.next()
			return &Unary{Op: "not", Operand: operand}, nil
		case "if":
			return p.parseIf()
		}
		return nil, fmt.Errorf("feel: %s cannot start an expression", tok)

	case tokName:
		p.next()
		if p.at(tokLParen) {
			return p.parseCall(tok.text)
		}
		return &Name{Text: tok.text}, nil

	case tokOperator:
		if tok.text == "-" {
			p.next()
			operand, err := p.parseExpr(bpUnary)
			if err != nil {
				return nil, err
			}
			return &Unary{Op: "-", Operand: operand}, nil
		}
		return nil, fmt.Errorf("feel: %s cannot start an expression", tok)

	case tokLParen, tokLBracket:
		return p.parseParenOrRangeOrList()

	case tokRBracket:
		// `]10..20]` is the other FEEL spelling of an exclusive lower bound, and
		// the one DMN decision tables are written in — it is what Camunda
		// exports and what this product's own editor offers from its cell menu.
		// A `]` cannot open anything else, so requiring a range here costs
		// nothing and gives a better message when it is a typo.
		return p.parseOpenLowRange()

	case tokLBrace:
		return p.parseContext()
	}

	return nil, fmt.Errorf("feel: %s cannot start an expression", tok)
}

// parseParenOrRangeOrList handles the three constructs that begin with a
// bracket. They are only distinguishable after parsing the first element and
// looking for `..`, so they share one entry point.
func (p *parser) parseParenOrRangeOrList() (Node, error) {
	open := p.next()
	openBracket := open.kind == tokLBracket

	// `[]` is the empty list.
	if openBracket && p.at(tokRBracket) {
		p.next()
		return &ListNode{}, nil
	}

	first, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}

	switch {
	case p.at(tokRange):
		p.next()
		// `[` and `(` at the start include and exclude the low end respectively.
		return p.finishRange(first, !openBracket)

	case openBracket:
		items := []Node{first}
		for p.at(tokComma) {
			p.next()
			item, err := p.parseExpr(bpNone)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if !p.at(tokRBracket) {
			return nil, fmt.Errorf("feel: list is missing its closing bracket")
		}
		p.next()
		return &ListNode{Items: items}, nil

	default:
		if !p.at(tokRParen) {
			return nil, fmt.Errorf("feel: parenthesis is not closed")
		}
		p.next()
		return first, nil
	}
}

func (p *parser) parseContext() (Node, error) {
	p.next() // {
	ctx := &ContextNode{}

	if p.at(tokRBrace) {
		p.next()
		return ctx, nil
	}
	for {
		keyTok := p.peek()
		if keyTok.kind != tokName && keyTok.kind != tokString {
			return nil, fmt.Errorf("feel: context key must be a name or string, found %s", keyTok)
		}
		p.next()
		if !p.at(tokColon) {
			return nil, fmt.Errorf("feel: context entry %q needs a colon", keyTok.text)
		}
		p.next()

		value, err := p.parseExpr(bpNone)
		if err != nil {
			return nil, err
		}
		ctx.Keys = append(ctx.Keys, keyTok.text)
		ctx.Values = append(ctx.Values, value)

		if p.at(tokComma) {
			p.next()
			continue
		}
		break
	}
	if !p.at(tokRBrace) {
		return nil, fmt.Errorf("feel: context is missing its closing brace")
	}
	p.next()
	return ctx, nil
}

func (p *parser) parseCall(name string) (Node, error) {
	p.next() // (
	call := &Call{Name: name}

	if p.at(tokRParen) {
		p.next()
		return call, nil
	}
	for {
		arg, err := p.parseExpr(bpNone)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)

		if p.at(tokComma) {
			p.next()
			continue
		}
		break
	}
	if !p.at(tokRParen) {
		return nil, fmt.Errorf("feel: call to %s is missing its closing parenthesis", name)
	}
	p.next()
	return call, nil
}

func (p *parser) parseIf() (Node, error) {
	p.next() // if
	cond, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	if !p.atKeyword("then") {
		return nil, fmt.Errorf("feel: if … needs a then")
	}
	p.next()
	thenNode, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	if !p.atKeyword("else") {
		return nil, fmt.Errorf("feel: if … then … needs an else")
	}
	p.next()
	elseNode, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	return &If{Cond: cond, Then: thenNode, Else: elseNode}, nil
}

func (p *parser) infixBindingPower() int {
	tok := p.peek()
	switch tok.kind {
	case tokOperator:
		switch tok.text {
		case "=", "!=", "<", "<=", ">", ">=":
			return bpCompare
		case "+", "-":
			return bpSum
		case "*", "/":
			return bpProduct
		case "**":
			return bpPower
		case ".":
			return bpPostfix
		}
	case tokKeyword:
		switch tok.text {
		case "and":
			return bpAnd
		case "or":
			return bpOr
		case "in", "between":
			return bpCompare
		}
	case tokLBracket:
		// Inside a range's upper bound a `[` is the closing bracket of an
		// exclusive range, not the start of an index. See finishRange.
		if p.inRangeBound > 0 {
			return bpNone
		}
		return bpPostfix
	}
	return bpNone
}

func (p *parser) parseInfix(left Node, bp int) (Node, error) {
	tok := p.next()

	switch {
	case tok.kind == tokOperator && tok.text == ".":
		field := p.peek()
		if field.kind != tokName {
			return nil, fmt.Errorf("feel: a property name must follow '.', found %s", field)
		}
		p.next()
		return &Path{Target: left, Field: field.text}, nil

	case tok.kind == tokLBracket:
		index, err := p.parseExpr(bpNone)
		if err != nil {
			return nil, err
		}
		if !p.at(tokRBracket) {
			return nil, fmt.Errorf("feel: index is missing its closing bracket")
		}
		p.next()
		return &Index{Target: left, Index: index}, nil

	case tok.kind == tokKeyword && tok.text == "in":
		target, err := p.parseExpr(bp)
		if err != nil {
			return nil, err
		}
		return &InNode{Value: left, Target: target}, nil

	case tok.kind == tokKeyword && tok.text == "between":
		low, err := p.parseExpr(bpCompare)
		if err != nil {
			return nil, err
		}
		if !p.atKeyword("and") {
			return nil, fmt.Errorf("feel: between … needs an and")
		}
		p.next()
		high, err := p.parseExpr(bpCompare)
		if err != nil {
			return nil, err
		}
		// `x between a and b` is inclusive on both ends.
		return &InNode{Value: left, Target: &RangeNode{Low: low, High: high}}, nil
	}

	// Left-associative: the right side binds at this level, so `1 - 2 - 3`
	// groups as `(1 - 2) - 3`.
	right, err := p.parseExpr(bp)
	if err != nil {
		return nil, err
	}
	return &Binary{Op: tok.text, Left: left, Right: right}, nil
}

// parseOpenLowRange reads `]low..high…`, whose leading bracket excludes the low
// end.
func (p *parser) parseOpenLowRange() (Node, error) {
	p.next() // ]
	low, err := p.parseExpr(bpNone)
	if err != nil {
		return nil, err
	}
	if !p.at(tokRange) {
		return nil, fmt.Errorf("feel: a range opened with ']' must be followed by '..'")
	}
	p.next()
	return p.finishRange(low, true)
}

// finishRange reads a range's upper bound and its closing bracket.
//
// Three closers are accepted: `]` includes the endpoint, and both `)` and `[`
// exclude it. The last is the DMN spelling — `[10..20[` — and it collides with
// the index operator, because `20[` looks like the start of `20[1]`. Indexing is
// suppressed while the bound is read, which is why this is not simply a call to
// parseExpr: nobody writes `[0..items[1][`, and everybody writes `[10..20[`.
func (p *parser) finishRange(low Node, lowOpen bool) (Node, error) {
	p.inRangeBound++
	high, err := p.parseExpr(bpNone)
	p.inRangeBound--
	if err != nil {
		return nil, err
	}

	closeTok := p.peek()
	switch closeTok.kind {
	case tokRBracket:
		p.next()
		return &RangeNode{Low: low, High: high, LowOpen: lowOpen}, nil
	case tokRParen, tokLBracket:
		p.next()
		return &RangeNode{Low: low, High: high, LowOpen: lowOpen, HighOpen: true}, nil
	default:
		return nil, fmt.Errorf("feel: range is missing its closing bracket")
	}
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) next() token {
	tok := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return tok
}

func (p *parser) at(kind tokenKind) bool { return p.tokens[p.pos].kind == kind }

func (p *parser) atKeyword(word string) bool {
	tok := p.tokens[p.pos]
	return tok.kind == tokKeyword && tok.text == word
}
