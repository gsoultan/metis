package grule

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// conditionColumn is the column a table uses for the parts of a condition
// that are not one field against a constant. The step maps it to true, and
// each line's cell is a FEEL test that must equal it.
const conditionColumn = "rule_condition"

// anyValue is the cell that matches whatever the column holds.
const anyValue = "-"

// buildTable makes one group's decision table.
func buildTable(g group, key string) entities.DecisionDefinition {
	columns := columnKeys(g)
	table := entities.DecisionDefinition{
		Key:       key,
		Name:      "Decide " + strings.Join(g.fields, ", "),
		HitPolicy: entities.HitPolicyFirst,
	}
	for _, column := range columns {
		table.Inputs = append(table.Inputs, inputColumn(column))
	}
	if needsConditionColumn(g) {
		table.Inputs = append(table.Inputs, entities.DecisionInput{
			ID: "in_" + conditionColumn, Label: "Other conditions", Expression: conditionColumn, Type: "boolean",
		})
	}
	for _, field := range g.fields {
		table.Outputs = append(table.Outputs, outputColumn(g, field))
	}
	for _, r := range g.rules {
		table.Rules = append(table.Rules, tableLine(g, r, columns))
	}
	return table
}

func inputColumn(key string) entities.DecisionInput {
	name := variableName(key)
	return entities.DecisionInput{ID: "in_" + name, Label: key, Expression: name}
}

// columnKeys are the fields compared with a constant somewhere in the group,
// in the order first compared.
func columnKeys(g group) []string {
	seen := map[string]bool{}
	var keys []string
	for _, r := range g.rules {
		for _, c := range r.conditions {
			if c.key != "" && !seen[c.key] {
				seen[c.key] = true
				keys = append(keys, c.key)
			}
		}
	}
	return keys
}

func needsConditionColumn(g group) bool {
	for _, r := range g.rules {
		if len(lineCells(r)) < len(r.conditions) {
			return true
		}
	}
	return false
}

// lineCells is the cell of each column a rule compares, the first comparison
// of a field only; a second comparison of the same field is left to the
// condition column.
func lineCells(r *analysedRule) map[string]string {
	cells := map[string]string{}
	for _, c := range r.conditions {
		if c.key != "" {
			if _, taken := cells[c.key]; !taken {
				cells[c.key] = c.cell
			}
		}
	}
	return cells
}

// otherConditions are the parts of a rule's condition the columns do not
// hold, as one FEEL test.
func otherConditions(r *analysedRule) string {
	taken := map[string]bool{}
	var parts []string
	for _, c := range r.conditions {
		if c.key != "" && !taken[c.key] {
			taken[c.key] = true
			continue
		}
		if c.key != "" {
			parts = append(parts, comparisonFEEL(c))
			continue
		}
		parts = append(parts, c.expression)
	}
	if len(parts) == 0 {
		return anyValue
	}
	return "(" + strings.Join(parts, " and ") + ")"
}

// comparisonFEEL writes a column comparison as a FEEL test of its own, for
// when it has to go in the condition column.
func comparisonFEEL(c condition) string {
	name := variableName(c.key)
	if strings.HasPrefix(c.cell, "=") || strings.HasPrefix(c.cell, "!=") ||
		strings.HasPrefix(c.cell, "<") || strings.HasPrefix(c.cell, ">") {
		return "(" + name + " " + c.cell + ")"
	}
	return "(" + name + " = " + c.cell + ")"
}

func tableLine(g group, r *analysedRule, columns []string) entities.DecisionRule {
	cells := lineCells(r)
	line := entities.DecisionRule{ID: r.Name, Description: strings.TrimSpace(r.Description + " (" + r.Position + ")")}
	for _, column := range columns {
		cell, ok := cells[column]
		if !ok {
			cell = anyValue
		}
		line.Inputs = append(line.Inputs, cell)
	}
	if needsConditionColumn(g) {
		line.Inputs = append(line.Inputs, otherConditions(r))
	}
	for _, field := range g.fields {
		line.Outputs = append(line.Outputs, outputCell(g, r, field))
	}
	return line
}

// outputColumn is a field the table answers. It is computed when any line
// sets it to something other than a constant.
func outputColumn(g group, field string) entities.DecisionOutput {
	name := variableName(field)
	return entities.DecisionOutput{
		ID: "out_" + name, Label: field, Name: name,
		Type: outputType(g, field), Computed: isComputed(g, field),
	}
}

func isComputed(g group, field string) bool {
	for _, r := range g.rules {
		if _, constant := r.values[field].(Literal); !constant {
			return true
		}
	}
	return false
}

func outputCell(g group, r *analysedRule, field string) any {
	if isComputed(g, field) {
		return r.feelValues[field]
	}
	// Not computed means every line sets a constant, so this always holds.
	if literal, ok := r.values[field].(Literal); ok {
		return literal.Value
	}
	return r.feelValues[field]
}

// outputType names the column's type when every line sets a constant of the
// same kind, and leaves it blank otherwise.
func outputType(g group, field string) string {
	kind := ""
	for _, r := range g.rules {
		literal, constant := r.values[field].(Literal)
		if !constant {
			return ""
		}
		this := ""
		switch literal.Value.(type) {
		case string:
			this = "string"
		case float64:
			this = "number"
		case bool:
			this = "boolean"
		}
		if kind != "" && kind != this {
			return ""
		}
		kind = this
	}
	return kind
}

// decisionKey is a table's key: the import's prefix and the fields it sets,
// lower case, made unique among the import's tables.
func decisionKey(prefix string, fields []string, taken map[string]bool) string {
	slug := strings.ToLower(strings.NewReplacer(".", "-", "_", "-").Replace(strings.Join(fields, "-")))
	if len(slug) > maxSlug {
		slug = slug[:maxSlug]
	}
	key := strings.Trim(prefix+"-"+slug, "-")
	for n := 2; taken[key]; n++ {
		key = fmt.Sprintf("%s-%s-%d", prefix, slug, n)
	}
	taken[key] = true
	return key
}

// maxSlug keeps a key short enough to read in a list.
const maxSlug = 48
