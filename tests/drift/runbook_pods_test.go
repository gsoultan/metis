package drift_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// A runbook command that selects pods by a label they do not carry answers "No
// resources found", which reads as "there are no pods" at exactly the moment
// somebody is asking why.
func TestTheRunbooksSelectPodsByLabelsThePodsCarry(t *testing.T) {
	stable := objectOfKind(t, manifestObjects(t, "deploy/kubernetes/metis.yaml"), "Deployment")
	labels, _ := field(stable, "spec", "template", "metadata", "labels").(map[string]any)

	runbooks, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..")), "docs/runbooks.md")
	if err != nil {
		t.Fatalf("read the runbooks: %v", err)
	}
	selectors := regexp.MustCompile(`kubectl [^\n]*-l ([^\s=]+)=([^\s|]+)`).FindAllStringSubmatch(string(runbooks), -1)
	if len(selectors) == 0 {
		t.Fatal("found no pod selectors in the runbooks; the pattern no longer matches how they are written")
	}
	for _, selector := range selectors {
		if labels[selector[1]] != selector[2] {
			t.Errorf("the runbooks select pods with %s=%s, which metis.yaml's pods do not carry (they carry %v)", selector[1], selector[2], labels)
		}
	}
}

// manifestObjects reads every document of a manifest, relative to the repository.
func manifestObjects(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..")), path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var objects []map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			return objects
		}
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if object != nil {
			objects = append(objects, object)
		}
	}
}

func objectOfKind(t *testing.T, objects []map[string]any, kind string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, object := range objects {
		if object["kind"] == kind {
			found = append(found, object)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one %s in the manifest, found %d", kind, len(found))
	}
	return found[0]
}

// field walks nested maps by key, answering nil where a key is missing.
func field(value any, path ...string) any {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}
