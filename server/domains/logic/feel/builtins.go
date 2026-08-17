package feel

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Now is the clock the temporal built-ins read.
//
// It is a variable so tests can freeze it. Production never reassigns it; the
// alternative — threading a clock through every evaluation — would put a
// parameter on the public API for the sake of two functions.
var Now = time.Now

// evalCall dispatches a built-in function.
//
// There are no user-defined functions, by design: a decision table is business
// policy authored by non-programmers, and every function this subset offers is
// total, bounded and side-effect free. That is what makes the language safe to
// hand untrusted input, which the JavaScript it replaces was not.
func (e *evaluator) evalCall(call *Call) (Value, error) {
	args := make([]Value, len(call.Args))
	for i, arg := range call.Args {
		value, err := e.eval(arg)
		if err != nil {
			return Null, err
		}
		args[i] = value
	}

	fn, ok := builtins[strings.ToLower(call.Name)]
	if !ok {
		return Null, fmt.Errorf("feel: there is no function called %q", call.Name)
	}
	if len(args) < fn.minArgs || (fn.maxArgs >= 0 && len(args) > fn.maxArgs) {
		return Null, fmt.Errorf("feel: %s takes %s, given %d", call.Name, fn.arity(), len(args))
	}
	return fn.call(args)
}

type builtin struct {
	minArgs, maxArgs int // maxArgs < 0 means variadic
	call             func(args []Value) (Value, error)
}

func (b builtin) arity() string {
	switch {
	case b.maxArgs < 0:
		return fmt.Sprintf("at least %d arguments", b.minArgs)
	case b.minArgs == b.maxArgs:
		return fmt.Sprintf("%d arguments", b.minArgs)
	default:
		return fmt.Sprintf("%d to %d arguments", b.minArgs, b.maxArgs)
	}
}

var builtins map[string]builtin

func init() {
	builtins = map[string]builtin{
		// --- conversion ---
		"date":          {1, 1, biDate},
		"time":          {1, 1, biTime},
		"date and time": {1, 1, biDateTime},
		"duration":      {1, 1, biDuration},
		"number":        {1, 1, biNumber},
		"string":        {1, 1, biString},

		// --- strings ---
		"contains":      {2, 2, biContains},
		"starts with":   {2, 2, biStartsWith},
		"ends with":     {2, 2, biEndsWith},
		"string length": {1, 1, biStringLength},
		"upper case":    {1, 1, biUpperCase},
		"lower case":    {1, 1, biLowerCase},
		"substring":     {2, 3, biSubstring},
		"matches":       {2, 2, biMatches},

		// --- lists ---
		"list contains": {2, 2, biListContains},
		"count":         {1, 1, biCount},
		"sum":           {1, -1, biSum},
		"min":           {1, -1, biMin},
		"max":           {1, -1, biMax},
		"mean":          {1, -1, biMean},
		"all":           {1, -1, biAll},
		"any":           {1, -1, biAny},
		"sublist":       {2, 3, biSublist},
		"append":        {1, -1, biAppend},
		"concatenate":   {1, -1, biConcatenate},

		// --- numbers ---
		"abs":     {1, 1, biAbs},
		"ceiling": {1, 1, biCeiling},
		"floor":   {1, 1, biFloor},
		"round":   {1, 2, biRound},
		"modulo":  {2, 2, biModulo},

		// --- temporal ---
		"today": {0, 0, biToday},
		"now":   {0, 0, biNow},

		// --- general ---
		"not": {1, 1, biNot},
	}
}

// ---------------------------------------------------------------- conversion

func biDate(args []Value) (Value, error) {
	switch args[0].Kind {
	case KindString:
		t, err := time.Parse("2006-01-02", args[0].Str)
		if err != nil {
			return Null, fmt.Errorf("feel: %q is not a date (expected YYYY-MM-DD)", args[0].Str)
		}
		return Value{Kind: KindDate, Time: t}, nil
	case KindDateTime:
		year, month, day := args[0].Time.Date()
		return Value{Kind: KindDate, Time: time.Date(year, month, day, 0, 0, 0, 0, args[0].Time.Location())}, nil
	}
	return Null, fmt.Errorf("feel: date() needs a string or date and time, found %s", args[0].Kind)
}

func biTime(args []Value) (Value, error) {
	if args[0].Kind != KindString {
		if args[0].Kind == KindDateTime {
			return Value{Kind: KindTime, Time: args[0].Time}, nil
		}
		return Null, fmt.Errorf("feel: time() needs a string, found %s", args[0].Kind)
	}
	for _, layout := range []string{"15:04:05", "15:04", time.RFC3339} {
		if t, err := time.Parse(layout, args[0].Str); err == nil {
			return Value{Kind: KindTime, Time: t}, nil
		}
	}
	return Null, fmt.Errorf("feel: %q is not a time (expected HH:MM:SS)", args[0].Str)
}

func biDateTime(args []Value) (Value, error) {
	if args[0].Kind != KindString {
		return Null, fmt.Errorf("feel: date and time() needs a string, found %s", args[0].Kind)
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, args[0].Str); err == nil {
			return Value{Kind: KindDateTime, Time: t}, nil
		}
	}
	return Null, fmt.Errorf("feel: %q is not a date and time", args[0].Str)
}

func biDuration(args []Value) (Value, error) {
	if args[0].Kind != KindString {
		return Null, fmt.Errorf("feel: duration() needs a string, found %s", args[0].Kind)
	}
	d, err := ParseDuration(args[0].Str)
	if err != nil {
		return Null, err
	}
	return Value{Kind: KindDuration, Duration: d}, nil
}

func biNumber(args []Value) (Value, error) {
	switch args[0].Kind {
	case KindNumber:
		return args[0], nil
	case KindString:
		f, err := strconv.ParseFloat(strings.TrimSpace(args[0].Str), 64)
		if err != nil {
			return Null, fmt.Errorf("feel: %q is not a number", args[0].Str)
		}
		return Num(f), nil
	}
	return Null, fmt.Errorf("feel: number() cannot convert a %s", args[0].Kind)
}

func biString(args []Value) (Value, error) { return Str(args[0].String()), nil }

// ------------------------------------------------------------------- strings

func needString(v Value, fn string) (string, error) {
	if v.Kind != KindString {
		return "", fmt.Errorf("feel: %s needs a string, found %s", fn, v.Kind)
	}
	return v.Str, nil
}

func biContains(args []Value) (Value, error) {
	s, err := needString(args[0], "contains")
	if err != nil {
		return Null, err
	}
	sub, err := needString(args[1], "contains")
	if err != nil {
		return Null, err
	}
	return Bool(strings.Contains(s, sub)), nil
}

func biStartsWith(args []Value) (Value, error) {
	s, err := needString(args[0], "starts with")
	if err != nil {
		return Null, err
	}
	prefix, err := needString(args[1], "starts with")
	if err != nil {
		return Null, err
	}
	return Bool(strings.HasPrefix(s, prefix)), nil
}

func biEndsWith(args []Value) (Value, error) {
	s, err := needString(args[0], "ends with")
	if err != nil {
		return Null, err
	}
	suffix, err := needString(args[1], "ends with")
	if err != nil {
		return Null, err
	}
	return Bool(strings.HasSuffix(s, suffix)), nil
}

func biStringLength(args []Value) (Value, error) {
	s, err := needString(args[0], "string length")
	if err != nil {
		return Null, err
	}
	return Num(float64(len([]rune(s)))), nil
}

func biUpperCase(args []Value) (Value, error) {
	s, err := needString(args[0], "upper case")
	if err != nil {
		return Null, err
	}
	return Str(strings.ToUpper(s)), nil
}

func biLowerCase(args []Value) (Value, error) {
	s, err := needString(args[0], "lower case")
	if err != nil {
		return Null, err
	}
	return Str(strings.ToLower(s)), nil
}

func biSubstring(args []Value) (Value, error) {
	s, err := needString(args[0], "substring")
	if err != nil {
		return Null, err
	}
	if args[1].Kind != KindNumber {
		return Null, fmt.Errorf("feel: substring position must be a number")
	}
	runes := []rune(s)

	// FEEL positions start at 1; negative counts from the end.
	start := int(args[1].Number)
	switch {
	case start > 0:
		start--
	case start < 0:
		start = len(runes) + start
	default:
		return Null, fmt.Errorf("feel: substring position starts at 1, not 0")
	}
	if start < 0 || start > len(runes) {
		return Str(""), nil
	}

	end := len(runes)
	if len(args) == 3 {
		if args[2].Kind != KindNumber {
			return Null, fmt.Errorf("feel: substring length must be a number")
		}
		end = start + int(args[2].Number)
		if end > len(runes) {
			end = len(runes)
		}
		if end < start {
			return Str(""), nil
		}
	}
	return Str(string(runes[start:end])), nil
}

// biMatches is deliberately a literal-substring test, not a regular
// expression.
//
// FEEL defines matches() over XPath regular expressions, and a regex compiled
// from a deployed definition is an attacker-supplied pattern: catastrophic
// backtracking would hang the goroutine evaluating it, which is the class of
// problem this whole package exists to remove. Rather than ship a
// denial-of-service vector under a familiar name, this matches literally and
// says so.
func biMatches(args []Value) (Value, error) {
	s, err := needString(args[0], "matches")
	if err != nil {
		return Null, err
	}
	pattern, err := needString(args[1], "matches")
	if err != nil {
		return Null, err
	}
	return Bool(strings.Contains(s, pattern)), nil
}

// --------------------------------------------------------------------- lists

// listArgs accepts either a single list argument or several loose ones, which
// is how FEEL's aggregates are written: sum([1,2,3]) and sum(1,2,3) both work.
func listArgs(args []Value) []Value {
	if len(args) == 1 && args[0].Kind == KindList {
		return args[0].List
	}
	return args
}

func biListContains(args []Value) (Value, error) {
	if args[0].Kind != KindList {
		return Null, fmt.Errorf("feel: list contains needs a list, found %s", args[0].Kind)
	}
	for _, item := range args[0].List {
		if equal(item, args[1]) {
			return True, nil
		}
	}
	return False, nil
}

func biCount(args []Value) (Value, error) {
	if args[0].Kind != KindList {
		return Null, fmt.Errorf("feel: count needs a list, found %s", args[0].Kind)
	}
	return Num(float64(len(args[0].List))), nil
}

func numbersOf(values []Value, fn string) ([]float64, error) {
	out := make([]float64, 0, len(values))
	for _, v := range values {
		if v.Kind != KindNumber {
			return nil, fmt.Errorf("feel: %s needs numbers, found a %s", fn, v.Kind)
		}
		out = append(out, v.Number)
	}
	return out, nil
}

func biSum(args []Value) (Value, error) {
	numbers, err := numbersOf(listArgs(args), "sum")
	if err != nil {
		return Null, err
	}
	total := 0.0
	for _, n := range numbers {
		total += n
	}
	return Num(total), nil
}

func biMean(args []Value) (Value, error) {
	numbers, err := numbersOf(listArgs(args), "mean")
	if err != nil {
		return Null, err
	}
	if len(numbers) == 0 {
		return Null, nil
	}
	total := 0.0
	for _, n := range numbers {
		total += n
	}
	return Num(total / float64(len(numbers))), nil
}

func biMin(args []Value) (Value, error) {
	values := listArgs(args)
	if len(values) == 0 {
		return Null, nil
	}
	best := values[0]
	for _, v := range values[1:] {
		cmp, err := order(v, best)
		if err != nil {
			return Null, err
		}
		if cmp < 0 {
			best = v
		}
	}
	return best, nil
}

func biMax(args []Value) (Value, error) {
	values := listArgs(args)
	if len(values) == 0 {
		return Null, nil
	}
	best := values[0]
	for _, v := range values[1:] {
		cmp, err := order(v, best)
		if err != nil {
			return Null, err
		}
		if cmp > 0 {
			best = v
		}
	}
	return best, nil
}

func biAll(args []Value) (Value, error) {
	for _, v := range listArgs(args) {
		if v.Kind != KindBoolean {
			return Null, fmt.Errorf("feel: all() needs booleans, found a %s", v.Kind)
		}
		if !v.Bool {
			return False, nil
		}
	}
	return True, nil
}

func biAny(args []Value) (Value, error) {
	for _, v := range listArgs(args) {
		if v.Kind != KindBoolean {
			return Null, fmt.Errorf("feel: any() needs booleans, found a %s", v.Kind)
		}
		if v.Bool {
			return True, nil
		}
	}
	return False, nil
}

func biSublist(args []Value) (Value, error) {
	if args[0].Kind != KindList {
		return Null, fmt.Errorf("feel: sublist needs a list, found %s", args[0].Kind)
	}
	if args[1].Kind != KindNumber {
		return Null, fmt.Errorf("feel: sublist position must be a number")
	}
	items := args[0].List

	start := int(args[1].Number)
	switch {
	case start > 0:
		start--
	case start < 0:
		start = len(items) + start
	default:
		return Null, fmt.Errorf("feel: sublist position starts at 1, not 0")
	}
	if start < 0 || start > len(items) {
		return List(), nil
	}

	end := len(items)
	if len(args) == 3 {
		if args[2].Kind != KindNumber {
			return Null, fmt.Errorf("feel: sublist length must be a number")
		}
		end = start + int(args[2].Number)
		if end > len(items) {
			end = len(items)
		}
		if end < start {
			return List(), nil
		}
	}
	return Value{Kind: KindList, List: append([]Value(nil), items[start:end]...)}, nil
}

func biAppend(args []Value) (Value, error) {
	if args[0].Kind != KindList {
		return Null, fmt.Errorf("feel: append needs a list, found %s", args[0].Kind)
	}
	out := append([]Value(nil), args[0].List...)
	out = append(out, args[1:]...)
	return Value{Kind: KindList, List: out}, nil
}

func biConcatenate(args []Value) (Value, error) {
	var out []Value
	for _, arg := range args {
		if arg.Kind != KindList {
			return Null, fmt.Errorf("feel: concatenate needs lists, found a %s", arg.Kind)
		}
		out = append(out, arg.List...)
	}
	return Value{Kind: KindList, List: out}, nil
}

// ------------------------------------------------------------------- numbers

func needNumber(v Value, fn string) (float64, error) {
	if v.Kind != KindNumber {
		return 0, fmt.Errorf("feel: %s needs a number, found %s", fn, v.Kind)
	}
	return v.Number, nil
}

func biAbs(args []Value) (Value, error) {
	n, err := needNumber(args[0], "abs")
	if err != nil {
		return Null, err
	}
	return Num(math.Abs(n)), nil
}

func biCeiling(args []Value) (Value, error) {
	n, err := needNumber(args[0], "ceiling")
	if err != nil {
		return Null, err
	}
	return Num(math.Ceil(n)), nil
}

func biFloor(args []Value) (Value, error) {
	n, err := needNumber(args[0], "floor")
	if err != nil {
		return Null, err
	}
	return Num(math.Floor(n)), nil
}

func biRound(args []Value) (Value, error) {
	n, err := needNumber(args[0], "round")
	if err != nil {
		return Null, err
	}
	places := 0.0
	if len(args) == 2 {
		if places, err = needNumber(args[1], "round"); err != nil {
			return Null, err
		}
	}
	scale := math.Pow(10, places)
	return Num(math.Round(n*scale) / scale), nil
}

func biModulo(args []Value) (Value, error) {
	a, err := needNumber(args[0], "modulo")
	if err != nil {
		return Null, err
	}
	b, err := needNumber(args[1], "modulo")
	if err != nil {
		return Null, err
	}
	if b == 0 {
		return Null, fmt.Errorf("feel: modulo by zero")
	}
	return Num(math.Mod(a, b)), nil
}

// ------------------------------------------------------------------ temporal

func biToday([]Value) (Value, error) {
	now := Now()
	year, month, day := now.Date()
	return Value{Kind: KindDate, Time: time.Date(year, month, day, 0, 0, 0, 0, now.Location())}, nil
}

func biNow([]Value) (Value, error) {
	return Value{Kind: KindDateTime, Time: Now()}, nil
}

func biNot(args []Value) (Value, error) {
	if args[0].Kind != KindBoolean {
		return Null, fmt.Errorf("feel: not() needs a boolean, found %s", args[0].Kind)
	}
	return Bool(!args[0].Bool), nil
}

// ParseDuration parses an ISO-8601 duration.
//
// Both flavours are recognised and kept apart: P1Y2M is year-month, PT3H is
// day-time. Mixing them (P1Y2MT3H) is rejected rather than silently truncated,
// because a duration whose length depends on the date it is added to cannot be
// reduced to nanoseconds up front.
func ParseDuration(s string) (Duration, error) {
	original := s
	d := Duration{}

	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-") {
		d.Neg = true
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return d, fmt.Errorf("feel: %q is not a duration (expected ISO-8601 like PT5M or P1Y)", original)
	}
	s = s[1:]

	datePart, timePart, hasTime := strings.Cut(s, "T")

	years, months, days, err := parseDateDesignators(datePart, original)
	if err != nil {
		return d, err
	}

	var elapsed time.Duration
	if hasTime {
		if elapsed, err = parseTimeDesignators(timePart, original); err != nil {
			return d, err
		}
	}

	switch {
	case (years != 0 || months != 0) && (days != 0 || elapsed != 0):
		return d, fmt.Errorf("feel: %q mixes years or months with days or time; "+
			"a year-month duration has no fixed length, so the two cannot be combined", original)

	case years != 0 || months != 0:
		d.YearMonth = true
		d.Months = years*12 + months

	default:
		d.Elapsed = time.Duration(days)*24*time.Hour + elapsed
	}

	if datePart == "" && !hasTime {
		return d, fmt.Errorf("feel: %q is an empty duration", original)
	}
	return d, nil
}

func parseDateDesignators(s, original string) (years, months, days int, err error) {
	number := ""
	for _, c := range s {
		if c >= '0' && c <= '9' {
			number += string(c)
			continue
		}
		value, convErr := strconv.Atoi(number)
		if convErr != nil {
			return 0, 0, 0, fmt.Errorf("feel: %q has a malformed duration", original)
		}
		switch c {
		case 'Y':
			years = value
		case 'M':
			months = value
		case 'D':
			days = value
		case 'W':
			days += value * 7
		default:
			return 0, 0, 0, fmt.Errorf("feel: %q has an unknown duration unit %q", original, string(c))
		}
		number = ""
	}
	if number != "" {
		return 0, 0, 0, fmt.Errorf("feel: %q has a number with no unit", original)
	}
	return years, months, days, nil
}

func parseTimeDesignators(s, original string) (time.Duration, error) {
	var total time.Duration
	number := ""
	for _, c := range s {
		if (c >= '0' && c <= '9') || c == '.' {
			number += string(c)
			continue
		}
		value, convErr := strconv.ParseFloat(number, 64)
		if convErr != nil {
			return 0, fmt.Errorf("feel: %q has a malformed duration", original)
		}
		switch c {
		case 'H':
			total += time.Duration(value * float64(time.Hour))
		case 'M':
			total += time.Duration(value * float64(time.Minute))
		case 'S':
			total += time.Duration(value * float64(time.Second))
		default:
			return 0, fmt.Errorf("feel: %q has an unknown duration unit %q", original, string(c))
		}
		number = ""
	}
	if number != "" {
		return 0, fmt.Errorf("feel: %q has a number with no unit", original)
	}
	return total, nil
}
