package entities_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Known answers from a list kept beside the declarations, and a type declared
// but left off it would be refused at deploy. This reads the declarations
// themselves and holds the list to them.
func TestNodeTypesAreTheDeclaredOnes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "node_type.go", nil, 0)
	if err != nil {
		t.Fatalf("parse node_type.go: %v", err)
	}
	var declared []entities.NodeType
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "NodeType" {
			return true
		}
		for _, value := range spec.Values {
			lit, ok := value.(*ast.BasicLit)
			if !ok {
				t.Fatalf("a NodeType constant is not a string literal: %T", value)
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", lit.Value, err)
			}
			declared = append(declared, entities.NodeType(name))
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no NodeType constants in node_type.go")
	}

	listed := entities.NodeTypes()
	for _, nodeType := range declared {
		if !slices.Contains(listed, nodeType) {
			t.Errorf("%s is declared but not listed, so deploy would refuse it", nodeType)
		}
	}
	if len(listed) != len(declared) {
		t.Errorf("%d types are listed and %d declared", len(listed), len(declared))
	}
}

func TestATypeTheEngineDoesNotDeclareIsNotKnown(t *testing.T) {
	for _, nodeType := range []entities.NodeType{"", "sendTask", "receiveTask", "task", "StartEvent"} {
		if nodeType.Known() {
			t.Errorf("%q is taken for a type the engine declares", nodeType)
		}
	}
}
