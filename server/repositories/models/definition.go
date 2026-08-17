package models

// NodeType represents the type of a BPMN element in the database.
type NodeType string

const (
	StartEvent             NodeType = "startEvent"
	EndEvent               NodeType = "endEvent"
	UserTask               NodeType = "userTask"
	ServiceTask            NodeType = "serviceTask"
	ExclusiveGateway       NodeType = "exclusiveGateway"
	ParallelGateway        NodeType = "parallelGateway"
	InclusiveGateway       NodeType = "inclusiveGateway"
	ScriptTask             NodeType = "scriptTask"
	IntermediateCatchEvent NodeType = "intermediateCatchEvent"
	IntermediateThrowEvent NodeType = "intermediateThrowEvent"
	CallActivity           NodeType = "callActivity"
	ManualTask             NodeType = "manualTask"
	BusinessRuleTask       NodeType = "businessRuleTask"
	SubProcess             NodeType = "subProcess"
	BoundaryEvent          NodeType = "boundaryEvent"
	EventBasedGateway      NodeType = "eventBasedGateway"
	MessageEvent           NodeType = "messageEvent"
	SignalEvent            NodeType = "signalEvent"
	TimerEvent             NodeType = "timerEvent"
	ErrorEndEvent          NodeType = "errorEndEvent"
	TerminateEndEvent      NodeType = "terminateEndEvent"
	EscalationThrowEvent   NodeType = "escalationThrowEvent"
	CompensationThrowEvent NodeType = "compensationThrowEvent"
	Pool                   NodeType = "pool"
	Lane                   NodeType = "lane"
)

// FlowNode represents a node in a BPMN process definition in the database.
type FlowNode struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Type                NodeType `json:"type"`
	Assignee            string   `json:"assignee,omitzero"`
	CandidateUsers      []string `json:"candidate_users,omitzero"`
	CandidateGroups     []string `json:"candidate_groups,omitzero"`
	Priority            int      `json:"priority,omitzero"`
	DueDate             string   `json:"due_date,omitzero"`
	FormKey             string   `json:"form_key,omitzero"`
	DefaultFlow         string   `json:"default_flow,omitzero"`
	Script              string   `json:"script,omitzero"`
	ScriptFormat        string   `json:"script_format,omitzero"`
	ExternalTopic       string   `json:"external_topic,omitzero"`
	Documentation       string   `json:"documentation,omitzero"`
	AttachedToRef       string   `json:"attachedToRef,omitzero"`
	ParentID            string   `json:"parent_id,omitzero"`
	CancelActivity      bool     `json:"cancel_activity,omitzero"`
	MultiInstanceType   string   `json:"multi_instance_type,omitzero"` // none, parallel, sequential
	LoopCardinality     int      `json:"loop_cardinality,omitzero"`
	Collection          string   `json:"collection,omitzero"`
	ElementVariable     string   `json:"element_variable,omitzero"`
	CompletionCondition string   `json:"completion_condition,omitzero"`

	// ErrorCode is the BPMN errorCode an error boundary event catches. It had
	// no column here, so it was dropped on save and every boundary event came
	// back as a catch-all: a path meant for "the card was declined" was taken
	// for a timeout, a bad URL, anything at all.
	ErrorCode         string         `json:"error_code,omitzero"`
	IsAdHoc           bool           `json:"is_ad_hoc,omitzero"`
	IsEventSubProcess bool           `json:"is_event_sub_process,omitzero"`
	Incoming          []string       `json:"incoming,omitzero"`
	Outgoing          []string       `json:"outgoing,omitzero"`
	X                 int            `json:"x,omitzero"`
	Y                 int            `json:"y,omitzero"`
	Condition         string         `json:"condition,omitzero"`
	Properties        map[string]any `json:"properties,omitzero" gorm:"serializer:json"`
	Nodes             []FlowNode     `json:"nodes,omitzero"`
	Flows             []SequenceFlow `json:"flows,omitzero"`
}

// SequenceFlow represents a connection between two nodes in the database.
type SequenceFlow struct {
	ID            string `json:"id"`
	SourceRef     string `json:"source_ref"`
	TargetRef     string `json:"target_ref"`
	Condition     string `json:"condition,omitzero"`
	Documentation string `json:"documentation,omitzero"`
}

// ProcessDefinitionModel represents the GORM model for process definitions.
type ProcessDefinitionModel struct {
	Base
	// (project_id, key, version) is unique, and the uniqueness is the whole
	// point: a version is allocated by reading the highest one and adding one,
	// which two concurrent deployments of the same process will do from the
	// same starting number. Without the constraint both writes succeed, two
	// rows claim to be version N, and which one a "start version N" call gets
	// is a coin flip. With it the loser is told to try again.
	//
	// The constraint is declared by a migration rather than a `uniqueIndex` tag
	// here. A tag would put it in the baseline AutoMigrate, which runs before
	// the migration that repairs pre-existing duplicates — so the upgrade would
	// fail on exactly the installations that need it.
	ProjectID    UUID           `gorm:"index" json:"project_id,omitzero"`
	Key          string         `gorm:"size:255;index" json:"key"`
	Name         string         `json:"name"`
	Version      int            `json:"version"`
	Nodes        []FlowNode     `gorm:"type:text;serializer:json" json:"nodes,omitzero"`
	Flows        []SequenceFlow `gorm:"type:text;serializer:json" json:"flows,omitzero"`
	DeploymentID UUID           `gorm:"index" json:"deployment_id,omitzero"`
}

// TableName overrides the table name for ProcessDefinitionModel.
func (ProcessDefinitionModel) TableName() string {
	return "process_definitions"
}
