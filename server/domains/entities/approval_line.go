package entities

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// An approval line is the shape most processes people build first: somebody
// submits a request, it passes a fixed list of approvers in order, and each
// approves it, rejects it, or sends it back to be revised.
//
// It is defined as data, and metis turns it into an ordinary process
// definition: the BPMN, the gateways and the variables each decision is kept
// in are metis's, so the caller writes none of them. Everything a definition
// gets — versions, scheduled releases, migration, the stuck-task report —
// applies to it unchanged.

// What an approver or the submitter can do on an approval line's task.
const (
	ApprovalApprove  = "approve"
	ApprovalReject   = "reject"
	ApprovalRevise   = "revise"
	ApprovalResubmit = "resubmit"
	ApprovalWithdraw = "withdraw"
)

// Where an approval request is.
const (
	ApprovalInProgress = "in_progress"
	ApprovalSentBack   = "sent_back"
	ApprovalApproved   = "approved"
	ApprovalRejected   = "rejected"
	ApprovalWithdrawn  = "withdrawn"
	// ApprovalStopped is a request whose instance ended any other way: it was
	// cancelled, or it failed.
	ApprovalStopped = "stopped"
)

// Where one step of a request is.
const (
	ApprovalStepPending  = "pending"
	ApprovalStepWaiting  = "waiting"
	ApprovalStepApproved = "approved"
	ApprovalStepRejected = "rejected"
	ApprovalStepSentBack = "sent_back"
)

// The variables an approval line keeps its state in. Callers cannot set them:
// Submit and Decide refuse a variable with one of these names.
const (
	ApprovalVarDecision    = "approvalDecision"
	ApprovalVarTrail       = "approvalTrail"
	ApprovalVarRound       = "approvalRound"
	ApprovalVarSubmittedBy = "submittedBy"
)

// MaxApprovalRequestsListed bounds a list of a line's requests.
const MaxApprovalRequestsListed = 200

// MaxApprovalSteps bounds how many approvers one line has. A line longer than
// this is a process, and belongs in a modeler.
const MaxApprovalSteps = 20

// ApprovalLine is the definition: who approves, in what order.
type ApprovalLine struct {
	// Key is the process key requests are submitted under.
	Key  string `json:"key"`
	Name string `json:"name"`
	// Steps are the approvers, in order. Each one's approval moves the
	// request to the next; the last one's completes it.
	Steps []ApprovalStep `json:"steps"`
	// AllowRevise lets an approver send the request back to its submitter,
	// who resubmits it (it starts again at the first step) or withdraws it.
	AllowRevise bool `json:"allow_revise,omitzero"`
	// Fields are the request's own variables the submitter may correct when
	// it is sent back: the revise step's form. Submit takes any variables;
	// a resubmit only these.
	Fields []ApprovalField `json:"fields,omitzero"`
	// Version is the definition version this line was read from or deployed
	// as. Ignored when defining one.
	Version int `json:"version,omitzero"`
	// DefinitionID is the process definition the line is. Ignored when
	// defining one.
	DefinitionID uuid.UUID `json:"definition_id,omitzero"`
}

// ApprovalStep is one approver on a line: a task offered to people and
// groups, any one of whom decides.
type ApprovalStep struct {
	// ID names the step within the line: lower-case letters, digits and
	// dashes. It is what a request's line reports each step under.
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Users  []string `json:"users,omitzero"`
	Groups []string `json:"groups,omitzero"`
}

// ApprovalField is one variable of a request, as a form shows it.
type ApprovalField struct {
	ID    string `json:"id"`
	Label string `json:"label,omitzero"`
	// Type is text (the default), number, boolean or date.
	Type string `json:"type,omitzero"`
}

// ApprovalFieldTypes are the types a field may have.
var ApprovalFieldTypes = []string{"text", "number", "boolean", "date"}

var (
	approvalFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	approvalKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	approvalStepPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
)

// Validate says what is wrong with a line, or nil.
func (l ApprovalLine) Validate() error {
	if !approvalKeyPattern.MatchString(l.Key) {
		return fmt.Errorf("an approval line's key is lower-case letters, digits and dashes, starting with a letter: %q is not", l.Key)
	}
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("approval line %s needs a name", l.Key)
	}
	if len(l.Steps) == 0 {
		return fmt.Errorf("approval line %s has no steps: name at least one approver", l.Key)
	}
	if len(l.Steps) > MaxApprovalSteps {
		return fmt.Errorf("approval line %s has %d steps; at most %d", l.Key, len(l.Steps), MaxApprovalSteps)
	}
	fields := map[string]bool{}
	for _, f := range l.Fields {
		if !approvalFieldPattern.MatchString(f.ID) {
			return fmt.Errorf("field %q: a field id is letters, digits and underscores", f.ID)
		}
		if fields[f.ID] {
			return fmt.Errorf("two fields are called %s", f.ID)
		}
		fields[f.ID] = true
		switch f.ID {
		case ApprovalVarDecision, ApprovalVarTrail, ApprovalVarRound, ApprovalVarSubmittedBy:
			return fmt.Errorf("field %s is kept by the approval line itself", f.ID)
		}
		if f.Type != "" && !slices.Contains(ApprovalFieldTypes, f.Type) {
			return fmt.Errorf("field %s: type is one of %s", f.ID, strings.Join(ApprovalFieldTypes, ", "))
		}
	}
	if len(l.Fields) > 0 && !l.AllowRevise {
		return fmt.Errorf("fields are what a request sent back may correct: set allow_revise to use them")
	}
	seen := map[string]bool{}
	for i, s := range l.Steps {
		if !approvalStepPattern.MatchString(s.ID) {
			return fmt.Errorf("step %d: an id is lower-case letters, digits and dashes, starting with a letter: %q is not", i+1, s.ID)
		}
		if seen[s.ID] {
			return fmt.Errorf("two steps are called %s", s.ID)
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("step %s needs a name", s.ID)
		}
		named := 0
		for _, who := range append(append([]string{}, s.Users...), s.Groups...) {
			who = strings.TrimSpace(who)
			if who == "" {
				continue
			}
			if strings.ContainsAny(who, "${}#<>&\"',") {
				return fmt.Errorf("step %s: %q is not a user or group name", s.ID, who)
			}
			named++
		}
		if named == 0 {
			return fmt.Errorf("step %s names nobody: give it users or groups", s.ID)
		}
	}
	return nil
}

// ApprovalDecision is one decision on a request, as its trail keeps it.
type ApprovalDecision struct {
	Step     string    `json:"step"`
	Decision string    `json:"decision"`
	Comment  string    `json:"comment,omitzero"`
	By       string    `json:"by"`
	At       time.Time `json:"at"`
	Round    int       `json:"round"`
}

// ApprovalRequest is one request on a line and where it is.
type ApprovalRequest struct {
	InstanceID  uuid.UUID `json:"instance_id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Version     int       `json:"version"`
	Status      string    `json:"status"`
	SubmittedBy string    `json:"submitted_by,omitzero"`
	SubmittedAt time.Time `json:"submitted_at,omitzero"`
	// Round counts submissions: 2 after the first resubmit.
	Round int `json:"round"`
	// Steps are the line's steps with where each is, this round.
	Steps []ApprovalStepState `json:"steps"`
	// Revision is the open task sending the request back to its submitter,
	// while the request is sent back.
	Revision *ApprovalWaiting `json:"revision,omitzero"`
	// Trail is every decision made on it, every round, oldest first.
	Trail     []ApprovalDecision `json:"trail"`
	Variables map[string]any     `json:"variables,omitzero"`
}

// ApprovalStepState is one step of a request.
type ApprovalStepState struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Decision is this round's, once the step has decided.
	Decision *ApprovalDecision `json:"decision,omitzero"`
	// Waiting is the open task, while the step waits.
	Waiting *ApprovalWaiting `json:"waiting,omitzero"`
	Users   []string         `json:"users,omitzero"`
	Groups  []string         `json:"groups,omitzero"`
}

// ApprovalWaiting is the open task a request waits on, and for whom.
type ApprovalWaiting struct {
	TaskID   uuid.UUID `json:"task_id"`
	Assignee string    `json:"assignee,omitzero"`
	Users    []string  `json:"users,omitzero"`
	Groups   []string  `json:"groups,omitzero"`
	Since    time.Time `json:"since,omitzero"`
}

// ApprovalTakeover is what is still running on a line's older versions once a
// newer one is live: the requests submitted before the change, version by
// version, with what moving them onto the live version would do.
type ApprovalTakeover struct {
	Key         string           `json:"key"`
	LiveVersion int              `json:"live_version"`
	Behind      []ApprovalBehind `json:"behind"`
}

// ApprovalBehind is one older version's open requests.
type ApprovalBehind struct {
	Version      int               `json:"version"`
	DefinitionID uuid.UUID         `json:"definition_id"`
	Requests     []ApprovalRequest `json:"requests"`
	// Plan is what moving every one of them onto the live version would do,
	// as the migration preview says it: where each step's work lands, and
	// anything that would stop the move.
	Plan MigrationPlan `json:"plan"`
}

// ApprovalMoveResult is what a move of requests onto the live version did.
type ApprovalMoveResult struct {
	// Moved counts the requests now on the live version.
	Moved int `json:"moved"`
	// Left are the requests left where they were, each with why.
	Left []ApprovalLeft `json:"left,omitempty"`
}

// ApprovalLeft is a request a move left on its version.
type ApprovalLeft struct {
	InstanceID uuid.UUID `json:"instance_id"`
	Why        string    `json:"why"`
}

// MaxApprovalRequestsMoved bounds one move, as a list of requests is bounded.
const MaxApprovalRequestsMoved = MaxApprovalRequestsListed
