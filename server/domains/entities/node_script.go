package entities

// ScriptSource is the script a script task runs: its Script, else its
// Condition, else a "script" property. Empty means the step has nothing to run.
//
// One place, because the handler that runs a script and the check that refuses
// one at deploy must agree on where it is: a check that read only Script would
// let a script carried in a property through, and it would still run.
func (n *Node) ScriptSource() string {
	if n.Script != "" {
		return n.Script
	}
	if n.Condition != "" {
		return n.Condition
	}
	if s, ok := n.Properties["script"].(string); ok {
		return s
	}
	return ""
}
