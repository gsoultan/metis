package task

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
)

// A due date in an edit is absent (leave it), empty (remove it) or a time (set
// it). A *time.Time cannot tell the first two apart, which is how an edit that
// did not mention the due date came to remove it.
func TestAnEditSaysWhichFieldsItCarries(t *testing.T) {
	t.Parallel()
	december := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		body    string
		present bool
		value   *time.Time
	}{
		{"no due date mentioned", `{"name": "Approve"}`, false, nil},
		{"a null due date", `{"due_date": null}`, true, nil},
		{"an empty due date, which is what the inbox sends for none", `{"due_date": ""}`, true, nil},
		{"a due date", `{"due_date": "2026-12-01T09:00:00Z"}`, true, &december},
		{"a due date in another zone", `{"due_date": "2026-12-01T16:00:00+07:00"}`, true, &december},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var req UpdateTaskRequest
			if err := json.Unmarshal([]byte(c.body), &req); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if req.DueDate.Present != c.present {
				t.Fatalf("Present = %v, want %v", req.DueDate.Present, c.present)
			}
			if (req.DueDate.Value == nil) != (c.value == nil) || (c.value != nil && !req.DueDate.Value.Equal(*c.value)) {
				t.Fatalf("Value = %v, want %v", req.DueDate.Value, c.value)
			}
		})
	}
}

func TestAnEditCarriesNameAndPriorityOnlyWhenTheyAreThere(t *testing.T) {
	t.Parallel()
	var none UpdateTaskRequest
	if err := json.Unmarshal([]byte(`{"reason": "why"}`), &none); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if none.Name != nil || none.Priority != nil || none.Reason != "why" {
		t.Fatalf("a body naming neither carries name %v and priority %v", none.Name, none.Priority)
	}
	var both UpdateTaskRequest
	if err := json.Unmarshal([]byte(`{"name": "", "priority": 0}`), &both); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if both.Name == nil || *both.Name != "" || both.Priority == nil || *both.Priority != 0 {
		t.Fatal("an empty name and a zero priority are values the request carried, and were read as absent")
	}
}

func TestADueDateTheServerCannotReadIsTheCallersMistake(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"due_date": "tomorrow"}`, `{"due_date": 5}`, `{"due_date": "2026-12-01"}`} {
		var req UpdateTaskRequest
		err := json.Unmarshal([]byte(body), &req)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s: got %v, want an invalid-argument error that says what to send", body, err)
		}
	}
}
