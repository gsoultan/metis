package releasemanifest

import (
	"bytes"
	"strings"
	"testing"
)

func sample() Inputs {
	return Inputs{
		Release:      "v0.9.0",
		Converted:    50,
		OldestSchema: 42,
		ViaRelease:   "0.6.0",
		Migrations:   []Migration{{52, "b"}, {51, "a"}, {53, "c"}},
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*Inputs)
		wantErr string
		latest  int64
	}{
		{name: "sorted and latest", edit: func(*Inputs) {}, latest: 53},
		{name: "no migrations after the conversion", edit: func(in *Inputs) { in.Migrations = nil }, latest: 50},
		{name: "empty release", edit: func(in *Inputs) { in.Release = "" }, wantErr: "release is empty"},
		{name: "duplicate", edit: func(in *Inputs) { in.Migrations = append(in.Migrations, Migration{52, "again"}) }, wantErr: "listed twice"},
		{name: "before the conversion", edit: func(in *Inputs) { in.Migrations = append(in.Migrations, Migration{49, "old"}) }, wantErr: "not after the conversion"},
		{name: "oldest above latest", edit: func(in *Inputs) { in.OldestSchema = 60 }, wantErr: "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := sample()
			tt.edit(&in)
			m, err := Build(in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.SchemaVersion != tt.latest {
				t.Fatalf("schema version = %d, want %d", m.SchemaVersion, tt.latest)
			}
			for i := 1; i < len(m.Migrations); i++ {
				if m.Migrations[i].Version <= m.Migrations[i-1].Version {
					t.Fatalf("migrations not sorted: %+v", m.Migrations)
				}
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	m, err := Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, m); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Release != "v0.9.0" || got.SchemaVersion != 53 || len(got.Migrations) != 3 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestReadRefuses(t *testing.T) {
	tests := []struct{ name, body string }{
		{"unknown format", `{"format":2,"release":"v1","schema_version":53,"oldest_schema":42,"via_release":"0.6.0","migrations":[]}`},
		{"inconsistent", `{"format":1,"release":"v1","schema_version":40,"oldest_schema":42,"via_release":"0.6.0","migrations":[]}`},
		{"not json", `<html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Read(strings.NewReader(tt.body)); err == nil {
				t.Fatal("expected the manifest to be refused")
			}
		})
	}
}

// A newer release may add fields; an older installation must still read it.
func TestReadIgnoresFieldsAddedLater(t *testing.T) {
	body := `{"format":1,"release":"v2.0.0","schema_version":60,"oldest_schema":42,"via_release":"0.6.0","migrations":[],"notes":"new"}`
	if _, err := Read(strings.NewReader(body)); err != nil {
		t.Fatalf("a field added later must not stop an upgrade: %v", err)
	}
}

func TestPendingAndReachable(t *testing.T) {
	m, err := Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		applied   int64
		pending   int
		reachable bool
	}{
		{0, 3, true},
		{36, 3, false},
		{42, 3, true},
		{51, 2, true},
		{53, 0, true},
	}
	for _, tt := range tests {
		if got := len(m.Pending(tt.applied)); got != tt.pending {
			t.Errorf("Pending(%d) = %d, want %d", tt.applied, got, tt.pending)
		}
		if got := m.Reachable(tt.applied); got != tt.reachable {
			t.Errorf("Reachable(%d) = %v, want %v", tt.applied, got, tt.reachable)
		}
	}
}
