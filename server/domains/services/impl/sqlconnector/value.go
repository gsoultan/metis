package sqlconnector

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
)

// maxExactDecimalDigits is how many significant digits a float64 carries
// without changing them. A decimal with more — an account number stored as
// NUMERIC, say — is kept as its text rather than rounded into another value.
const maxExactDecimalDigits = 15

// jsonValue turns one column's value into what a process variable holds: a
// string, a float64, a bool, nil, or parsed JSON.
//
// The engine evaluates the next gateway against the instance in memory, before
// the variables go through JSON, so a value has to be in its JSON shape already
// or a gateway and the step after it would see different types for one field.
//
// Numbers stay numbers, so a decision table comparing credit_limit > 1000 is
// comparing numbers — except one a float64 would change, which stays text,
// because a silently different number is worse than a string.
func jsonValue(databaseType string, raw any) any {
	switch v := raw.(type) {
	case nil, bool, float64:
		return v
	case string:
		return textValue(databaseType, v)
	case float32:
		return float64(v)
	case int64:
		return exactInteger(v)
	case time.Time:
		return timeText(databaseType, v)
	case []byte:
		return bytesValue(databaseType, v)
	}
	return nil
}

// exactInteger compares in integer space: converting first would round the
// very numbers this exists to catch, and the rounded one would pass.
func exactInteger(v int64) any {
	if -maxExactInteger <= v && v <= maxExactInteger {
		return float64(v)
	}
	return strconv.FormatInt(v, 10)
}

func timeText(databaseType string, v time.Time) string {
	switch strings.ToUpper(databaseType) {
	case "DATE":
		return v.Format(time.DateOnly)
	case "TIME":
		return v.Format("15:04:05.999999999")
	}
	return v.Format(time.RFC3339Nano)
}

// textValue reads a column the driver handed over as a string. pgx hands over
// NUMERIC this way, and a decision comparing credit_limit > 1000 has to be
// comparing numbers.
func textValue(databaseType, text string) any {
	switch strings.ToUpper(databaseType) {
	case "DECIMAL", "NUMERIC", "NEWDECIMAL", "MONEY", "SMALLMONEY":
		return decimalValue(text)
	case "JSON", "JSONB":
		return parsedJSON([]byte(text))
	}
	return text
}

// bytesValue reads a column the driver handed over as bytes, which is how
// MySQL delivers text and every driver delivers some types.
func bytesValue(databaseType string, b []byte) any {
	switch strings.ToUpper(databaseType) {
	case "DECIMAL", "NUMERIC", "NEWDECIMAL", "MONEY", "SMALLMONEY", "JSON", "JSONB":
		return textValue(databaseType, string(b))
	case "UNIQUEIDENTIFIER":
		// SQL Server stores the first three groups byte-swapped; read as text
		// they would not match the id anybody else sees.
		var id mssql.UniqueIdentifier
		if err := id.Scan(b); err != nil {
			return strings.ToUpper(string(b))
		}
		return id.String()
	case "BYTEA", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "IMAGE":
		return base64.StdEncoding.EncodeToString(b)
	}
	return string(b)
}

// parsedJSON is a JSON column as the structure it holds, or its text if it
// does not parse.
func parsedJSON(b []byte) any {
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return string(b)
	}
	return parsed
}

func decimalValue(text string) any {
	if significantDigits(text) > maxExactDecimalDigits {
		return text
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}
	return parsed
}

// significantDigits counts the digits that carry the value: not a sign, not
// leading zeros, not trailing zeros after the point.
func significantDigits(text string) int {
	integer, fraction, _ := strings.Cut(strings.TrimLeft(text, "+-"), ".")
	return len(strings.TrimLeft(integer+strings.TrimRight(fraction, "0"), "0"))
}
