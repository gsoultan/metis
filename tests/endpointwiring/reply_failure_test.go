package endpointwiring_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A reply that carries an error must say so, or the error is sent as success.
//
// The REST encoder reads a reply's error through Failed(). A reply type with an
// `error` field and no Failed() is encoded as it stands: the error marshals as
// `{}` and the status is 200. The five organization replies were like that, so
// reading, renaming or deleting another organization answered 200 `{"err":{}}`
// while nothing had happened. Nothing fails at compile time when a new reply
// forgets it, so this reads the source.
//
// Only exported types count: an unexported one is a reply's wire shape, written
// from an exported reply that has its own Failed().
func TestEveryReplyWithAnErrorReportsIt(t *testing.T) {
	root := filepath.Join(repoRoot(t), "server", "endpoints")
	carriesError, failed := map[string]bool{}, map[string]bool{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		collectReplies(filepath.Dir(path), file, carriesError, failed)
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	if len(carriesError) == 0 {
		t.Fatal("no reply with an error field found; this test is looking in the wrong place")
	}

	var silent []string
	for name := range carriesError {
		if !failed[name] {
			rel, _ := filepath.Rel(root, name)
			silent = append(silent, rel)
		}
	}
	sort.Strings(silent)
	if len(silent) > 0 {
		t.Fatalf("these replies carry an error but have no Failed(), so a failure is sent as 200:\n  %s",
			strings.Join(silent, "\n  "))
	}
}

// collectReplies records, keyed by package directory and type name, the
// exported structs with an error field and the types with a Failed method.
func collectReplies(dir string, file *ast.File, carriesError, failed map[string]bool) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok && typeSpec.Name.IsExported() && hasErrorField(typeSpec) {
					carriesError[filepath.Join(dir, typeSpec.Name.Name)] = true
				}
			}
		case *ast.FuncDecl:
			if receiver := receiverName(d); receiver != "" && d.Name.Name == "Failed" {
				failed[filepath.Join(dir, receiver)] = true
			}
		}
	}
}

func hasErrorField(spec *ast.TypeSpec) bool {
	structType, ok := spec.Type.(*ast.StructType)
	if !ok {
		return false
	}
	for _, field := range structType.Fields.List {
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "error" {
			return true
		}
	}
	return false
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
