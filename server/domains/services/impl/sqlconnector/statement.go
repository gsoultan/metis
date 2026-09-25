package sqlconnector

import "strings"

// maxStatementBytes bounds the query a step may carry. It is authored in the
// designer, so its length is somebody else's choice, and validation reads it
// twice.
const maxStatementBytes = 64 << 10

// validateStatement refuses a query that is anything but one read.
//
// It is read under every lexMode the dialect could mean, and has to be clean
// under all of them: see lexMode.
func validateStatement(statement string, d dialect) error {
	if strings.TrimSpace(statement) == "" {
		return refused(-1, "the step has no query")
	}
	if len(statement) > maxStatementBytes {
		return refused(-1, "the query is longer than %d KiB", maxStatementBytes>>10)
	}
	for _, mode := range d.lexModes() {
		tokens, err := lex(statement, mode)
		if err != nil {
			return err
		}
		if err := checkShape(tokens); err != nil {
			return err
		}
		if err := checkWords(tokens); err != nil {
			return err
		}
	}
	return nil
}

// checkShape holds a query to one statement that reads: its first word is
// SELECT or WITH, and a semicolon may only end it.
func checkShape(tokens []token) error {
	first := firstWord(tokens)
	if first == nil || (first.text != "select" && first.text != "with") {
		return refused(-1, "a lookup's query has to start with SELECT or WITH")
	}
	for i, t := range tokens {
		if t.kind == tokenSeparator && i != len(tokens)-1 {
			return refused(t.start, "a lookup runs one query; a semicolon may only end it")
		}
	}
	return nil
}

// firstWord skips the parentheses a query may open with.
func firstWord(tokens []token) *token {
	for i := range tokens {
		t := &tokens[i]
		if t.kind == tokenSymbol && t.text == "(" {
			continue
		}
		if t.kind == tokenWord {
			return t
		}
		return nil
	}
	return nil
}

func checkWords(tokens []token) error {
	for _, t := range tokens {
		if t.kind != tokenWord && t.kind != tokenQuotedIdentifier {
			continue
		}
		name := strings.ToLower(t.text)
		if why, forbidden := forbiddenWords[name]; forbidden {
			return refused(t.start, "%s %s; a lookup may only read", strings.ToUpper(name), why)
		}
	}
	return nil
}
