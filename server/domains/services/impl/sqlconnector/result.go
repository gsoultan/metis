package sqlconnector

// The shape a lookup's answer is stored in, under the step's result variable.
const (
	resultRows      = "rows"
	resultRow       = "row"
	resultRowCount  = "row_count"
	resultTruncated = "truncated"
)

// envelope is the one value a lookup stores.
//
// One value, under the name the step chose, because the engine writes every
// key a connector returns into the process's variables: returning rows and
// row_count at the top level would put them beside the process's own data,
// where the next lookup overwrites them and a variable of the same name is
// silently replaced.
//
// row is the first row, and is absent when there are none, so a step that
// expects one reads customer.row.tier and a gateway asks customer.row_count = 0.
// Finding nothing is not an error — "new customer" is a business answer, not an
// incident.
func (r rowsRead) envelope() map[string]any {
	rows := make([]any, len(r.rows))
	for i, row := range r.rows {
		rows[i] = row
	}
	out := map[string]any{
		resultRows:      rows,
		resultRowCount:  float64(len(r.rows)),
		resultTruncated: r.truncated,
	}
	if len(r.rows) > 0 {
		out[resultRow] = r.rows[0]
	}
	return out
}
