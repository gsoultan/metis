package impl

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// What a hand-over's audit entry keeps in its data, by key. The caller is the
// actor; who had the task and who has it now are recorded beside them rather
// than standing in for them.
const (
	auditActor             = "actor"
	auditTarget            = "target"
	auditPreviousHolder    = "previous_holder"
	auditHolder            = "holder"
	auditOwner             = "owner"
	auditReason            = "reason"
	auditCandidateOverride = "candidate_override"
	auditChanges           = "changes"
)

// editedFields are the fields an edit can change, in the order a sentence
// names them, with the words it names them by.
var editedFields = []struct{ key, words string }{
	{"name", "the name"},
	{"priority", "the priority"},
	{"due_date", "the due date"},
}

// handOverNarrative is the sentence for a task moving between people, or being
// edited: who did it, from whom, to whom, and why.
//
// It answers only for an entry that says who acted and what moved. An entry
// written before the trail kept those — an assignment naming only its target,
// a release naming nobody — reports false and keeps the sentence it always had.
func handOverNarrative(entry entities.AuditEntry) (string, bool) {
	actor := auditText(entry.Data, auditActor)
	if actor == "" {
		return "", false
	}
	subject := subjectName(entry)
	previous := auditText(entry.Data, auditPreviousHolder)
	target := auditText(entry.Data, auditTarget)

	var told string
	switch entry.Type {
	case EventTaskAssigned:
		told = assignedNarrative(actor, subject, previous, target)
	case EventTaskDelegated:
		told = movedNarrative("delegated", "", actor, subject, previous, target)
	case EventTaskResolved:
		told = movedNarrative("handed", " back", actor, subject, previous, target)
	case EventTaskUnclaimed:
		told = releasedNarrative(actor, subject, previous)
	case EventTaskEdited:
		told = editedNarrative(actor, subject, entry.Data[auditChanges])
	}
	if told == "" {
		return "", false
	}
	if override, ok := entry.Data[auditCandidateOverride].(bool); ok && override {
		told += ", who is not one of the people it is offered to"
	}
	if reason := auditText(entry.Data, auditReason); reason != "" {
		told += ": " + reason
	}
	return told, true
}

func assignedNarrative(actor, subject, previous, target string) string {
	switch {
	case target == "":
		return ""
	case previous == "":
		return fmt.Sprintf("%s assigned task %q to %s", actor, subject, target)
	}
	return fmt.Sprintf("%s reassigned task %q from %s to %s", actor, subject, previous, target)
}

// movedNarrative is a delegation or a hand-back: "<actor> <verb> task
// "<subject>"<after> [from <previous>] to <target>". Whoever had the task is
// named only when it was not the actor.
func movedNarrative(verb, after, actor, subject, previous, target string) string {
	switch {
	case target == "":
		return ""
	case previous == "" || previous == actor:
		return fmt.Sprintf("%s %s task %q%s to %s", actor, verb, subject, after, target)
	}
	return fmt.Sprintf("%s %s task %q%s from %s to %s", actor, verb, subject, after, previous, target)
}

func releasedNarrative(actor, subject, previous string) string {
	switch previous {
	case "":
		return ""
	case actor:
		return fmt.Sprintf("%s released task %q back to the queue", actor, subject)
	}
	return fmt.Sprintf("%s released task %q from %s back to the queue", actor, subject, previous)
}

func editedNarrative(actor, subject string, changes any) string {
	changed, ok := changes.(map[string]any)
	if !ok {
		return ""
	}
	var fields []string
	for _, field := range editedFields {
		if _, present := changed[field.key]; present {
			fields = append(fields, field.words)
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("%s changed %s of task %q", actor, inWords(fields), subject)
}

// inWords joins a list the way a sentence does: "a", "a and b", "a, b and c".
func inWords(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// auditText reads a string out of an entry's data, or "" when it is not there.
func auditText(data map[string]any, key string) string {
	if text, ok := data[key].(string); ok {
		return text
	}
	return ""
}
