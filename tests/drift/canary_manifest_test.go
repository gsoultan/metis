package drift_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// deploy/kubernetes/canary.yaml repeats metis.yaml's pod so that the two tracks
// differ in the release alone. Kept by hand, they drift: a variable added to
// one and not the other, and the canary measures the configuration instead of
// the release while its alerts compare the two as if it were the release.
func TestTheCanaryRunsTheStablePodWithAnotherImage(t *testing.T) {
	stable := manifestObjects(t, "deploy/kubernetes/metis.yaml")
	canary := manifestObjects(t, "deploy/kubernetes/canary.yaml")

	stableDeployment := objectOfKind(t, stable, "Deployment")
	canaryDeployment := objectOfKind(t, canary, "Deployment")
	service := objectOfKind(t, stable, "Service")

	stableLabels := field(stableDeployment, "spec", "template", "metadata", "labels")
	canaryLabels := field(canaryDeployment, "spec", "template", "metadata", "labels")
	if got := field(stableLabels, "track"); got != "stable" {
		t.Errorf("metis.yaml's pods carry track %v, want stable: the canary alerts compare the tracks by it", got)
	}
	if got := field(canaryLabels, "track"); got != "canary" {
		t.Errorf("canary.yaml's pods carry track %v, want canary: the canary alerts compare the tracks by it", got)
	}

	// Each Deployment selects its own pods and not the other's, or `kubectl
	// logs deploy/metis` can answer with the canary's logs.
	for name, deployment := range map[string]any{"metis.yaml": stableDeployment, "canary.yaml": canaryDeployment} {
		selector := field(deployment, "spec", "selector", "matchLabels")
		labels := field(deployment, "spec", "template", "metadata", "labels")
		if !reflect.DeepEqual(selector, labels) {
			t.Errorf("%s selects %v but its pods carry %v: a selector narrower than the labels matches the other track's pods too", name, selector, labels)
		}
	}

	// The Service sends requests to both tracks, so it selects on labels both
	// carry and never on the track.
	serviceSelector, _ := field(service, "spec", "selector").(map[string]any)
	if _, ok := serviceSelector["track"]; ok {
		t.Errorf("the Service selects on the track, so a canary would take no requests")
	}
	for key, want := range serviceSelector {
		if field(stableLabels, key) != want || field(canaryLabels, key) != want {
			t.Errorf("the Service selects %s=%v, which the stable and canary pods do not both carry", key, want)
		}
	}

	if diff := firstDifference("labels", withoutTrack(stableLabels), withoutTrack(canaryLabels)); diff != "" {
		t.Errorf("the pods' labels differ beyond the track: %s", diff)
	}
	if diff := firstDifference("annotations",
		field(stableDeployment, "spec", "template", "metadata", "annotations"),
		field(canaryDeployment, "spec", "template", "metadata", "annotations")); diff != "" {
		t.Errorf("the canary is not scraped like the stable pods: %s", diff)
	}
	if diff := firstDifference("spec.template.spec",
		withoutImages(t, field(stableDeployment, "spec", "template", "spec")),
		withoutImages(t, field(canaryDeployment, "spec", "template", "spec"))); diff != "" {
		t.Errorf("canary.yaml's pod is not metis.yaml's with another image: %s", diff)
	}
}

func withoutTrack(labels any) map[string]any {
	copied := map[string]any{}
	for key, value := range labels.(map[string]any) {
		if key != "track" {
			copied[key] = value
		}
	}
	return copied
}

// withoutImages copies a pod spec with every container's image blanked, the one
// field the tracks are meant to differ in.
func withoutImages(t *testing.T, spec any) any {
	t.Helper()
	data, err := yaml.Marshal(spec)
	if err != nil {
		t.Fatalf("copy the pod spec: %v", err)
	}
	var copied map[string]any
	if err := yaml.Unmarshal(data, &copied); err != nil {
		t.Fatalf("copy the pod spec: %v", err)
	}
	containers, _ := copied["containers"].([]any)
	for _, container := range containers {
		if c, ok := container.(map[string]any); ok {
			c["image"] = ""
		}
	}
	return copied
}

// firstDifference names the first path at which two parsed documents differ, so
// a failure says which field drifted rather than dumping both pods.
func firstDifference(path string, a, b any) string {
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s is %v in metis.yaml and %v in canary.yaml", path, a, b)
		}
		keys := make([]string, 0, len(a)+len(b))
		for key := range a {
			keys = append(keys, key)
		}
		for key := range b {
			if _, ok := a[key]; !ok {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			if diff := firstDifference(path+"."+key, a[key], b[key]); diff != "" {
				return diff
			}
		}
		return ""
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return fmt.Sprintf("%s is %v in metis.yaml and %v in canary.yaml", path, a, b)
		}
		for i := range a {
			if diff := firstDifference(fmt.Sprintf("%s[%d]", path, i), a[i], b[i]); diff != "" {
				return diff
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Sprintf("%s is %v in metis.yaml and %v in canary.yaml", path, a, b)
		}
		return ""
	}
}
