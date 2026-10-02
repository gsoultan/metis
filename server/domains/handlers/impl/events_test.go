package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// stubEngine implements every engine surface the event handlers depend on —
// both eventBusRunner and endEventEngine. Each hook succeeds by default; a test
// overrides only the call whose failure it is exercising, and `calls` records
// the order so tests can assert what ran before what.
type stubEngine struct {
	broadcastSignal  func(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error
	sendMessage      func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
	getInstance      func(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
	getDefinition    func(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	updateInstance   func(ctx context.Context, instance entities.ProcessInstance) error
	proceedIteration func(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID, iterationID string) error

	calls          []string
	sentMessages   []string
	sentCorrelKeys []string
}

func (s *stubEngine) record(call string) { s.calls = append(s.calls, call) }

func (s *stubEngine) StartProcess(context.Context, uuid.UUID, string, map[string]any) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (s *stubEngine) StartSubProcess(context.Context, uuid.UUID, string, int, map[string]any, uuid.UUID, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (s *stubEngine) ExecuteNode(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string) error {
	return nil
}

func (s *stubEngine) ExecuteNodeIteration(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string, string) error {
	return nil
}

func (s *stubEngine) Proceed(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string) error {
	return nil
}

func (s *stubEngine) ProceedIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID, iterationID string) error {
	s.record("ProceedIteration")
	if s.proceedIteration != nil {
		return s.proceedIteration(ctx, instance, def, nodeID, iterationID)
	}
	return nil
}

func (s *stubEngine) UpdateInstance(ctx context.Context, instance entities.ProcessInstance) error {
	s.record("UpdateInstance")
	if s.updateInstance != nil {
		return s.updateInstance(ctx, instance)
	}
	return nil
}

func (s *stubEngine) DispatchEvent(context.Context, entities.ProcessEvent) {
	s.record("DispatchEvent")
}

func (s *stubEngine) BroadcastSignal(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error {
	s.record("BroadcastSignal")
	if s.broadcastSignal != nil {
		return s.broadcastSignal(ctx, projectID, signalName, vars)
	}
	return nil
}

func (s *stubEngine) SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error {
	s.record("SendMessage")
	s.sentMessages = append(s.sentMessages, messageName)
	s.sentCorrelKeys = append(s.sentCorrelKeys, correlationKey)
	if s.sendMessage != nil {
		return s.sendMessage(ctx, projectID, messageName, correlationKey, vars)
	}
	return nil
}

func (s *stubEngine) TriggerEscalation(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, entities.Node, string) error {
	return nil
}

func (s *stubEngine) TriggerCompensation(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, entities.Node, string) error {
	return nil
}

func (s *stubEngine) GetInstance(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error) {
	s.record("GetInstance")
	if s.getInstance != nil {
		return s.getInstance(ctx, id)
	}
	return entities.ProcessInstance{ID: id, Definition: &entities.ProcessDefinition{ID: uuid.New()}}, nil
}

func (s *stubEngine) GetProcessDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error) {
	s.record("GetProcessDefinition")
	if s.getDefinition != nil {
		return s.getDefinition(ctx, id)
	}
	return &entities.ProcessDefinition{ID: id}, nil
}

func testInstance() *entities.ProcessInstance {
	return &entities.ProcessInstance{
		ID:        uuid.New(),
		Project:   &entities.Project{ID: uuid.New()},
		Variables: map[string]any{"orderId": "order-1"},
	}
}

// A signal that could not be broadcast must not be reported as thrown: the
// instances waiting on it stay parked, so advancing past the throw would strand
// them with no error recorded anywhere.
func TestSignalThrowEventHandlerPropagatesBroadcastFailure(t *testing.T) {
	wantErr := errors.New("subscription store unavailable")
	stub := &stubEngine{
		broadcastSignal: func(context.Context, uuid.UUID, string, map[string]any) error { return wantErr },
	}
	handler := &SignalThrowEventHandler{engine: stub}
	node := entities.Node{ID: "throw", Properties: map[string]any{"signal_name": "OrderApproved"}}

	err := handler.DoExecute(t.Context(), testInstance(), &entities.ProcessDefinition{}, node, "")

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the broadcast failure to surface, got %v", err)
	}
	for _, call := range stub.calls {
		if call == "ProceedIteration" {
			t.Error("the token advanced past the throw even though the signal was never broadcast")
		}
	}
}

// The same contract for a message throw.
func TestMessageThrowEventHandlerPropagatesSendFailure(t *testing.T) {
	wantErr := errors.New("subscription store unavailable")
	stub := &stubEngine{
		sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error { return wantErr },
	}
	handler := &MessageThrowEventHandler{engine: stub}
	node := entities.Node{ID: "throw", Properties: map[string]any{
		"message_name":    "PaymentReceived",
		"correlation_key": "${orderId}",
	}}

	err := handler.DoExecute(t.Context(), testInstance(), &entities.ProcessDefinition{}, node, "")

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the send failure to surface, got %v", err)
	}
	for _, call := range stub.calls {
		if call == "ProceedIteration" {
			t.Error("the token advanced past the throw even though the message was never sent")
		}
	}
}

// The throw side resolves its correlation key against instance variables, so a
// message leaves with the value that identifies the conversation rather than
// the template text.
func TestMessageThrowEventHandlerSendsResolvedCorrelationKey(t *testing.T) {
	stub := &stubEngine{}
	handler := &MessageThrowEventHandler{engine: stub}
	node := entities.Node{ID: "throw", Properties: map[string]any{
		"message_name":    "PaymentReceived",
		"correlation_key": "${orderId}",
	}}

	if err := handler.DoExecute(t.Context(), testInstance(), &entities.ProcessDefinition{}, node, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.sentCorrelKeys) != 1 || stub.sentCorrelKeys[0] != "order-1" {
		t.Errorf("expected the message to carry correlation key \"order-1\", got %v", stub.sentCorrelKeys)
	}
}

// An unresolvable correlation key must stop the throw. Sending with an empty key
// would broadcast a point-to-point message to every instance waiting on that
// message name.
func TestMessageThrowEventHandlerRejectsUnresolvableCorrelationKey(t *testing.T) {
	stub := &stubEngine{}
	handler := &MessageThrowEventHandler{engine: stub}
	node := entities.Node{ID: "throw", Properties: map[string]any{
		"message_name":    "PaymentReceived",
		"correlation_key": "${missingVariable}",
	}}

	err := handler.DoExecute(t.Context(), testInstance(), &entities.ProcessDefinition{}, node, "")

	if err == nil {
		t.Fatal("an unresolvable correlation key was accepted")
	}
	if !strings.Contains(err.Error(), "missingVariable") {
		t.Errorf("the error does not name the unresolved variable: %v", err)
	}
	if len(stub.sentMessages) != 0 {
		t.Errorf("a message was sent despite the unresolvable key: %v", stub.sentMessages)
	}
}

// A sub-process that finished but could not hand control back to its parent
// leaves the parent parked at the call activity forever. That has to surface.
func TestEndEventHandlerPropagatesParentResumeFailure(t *testing.T) {
	wantErr := errors.New("parent locked by another writer")
	stub := &stubEngine{
		proceedIteration: func(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string, string) error {
			return wantErr
		},
	}
	handler := &EndEventHandler{engine: stub}

	instance := testInstance()
	instance.ParentInstance = &entities.ProcessInstance{ID: uuid.New()}
	instance.ParentNode = &entities.Node{ID: "check"}

	err := handler.DoExecute(t.Context(), instance, &entities.ProcessDefinition{}, entities.Node{ID: "end"}, "")

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the parent resume failure to surface, got %v", err)
	}
}

// A parent that cannot even be loaded is the same failure one step earlier.
func TestEndEventHandlerPropagatesParentLoadFailure(t *testing.T) {
	wantErr := errors.New("parent instance not found")
	stub := &stubEngine{
		getInstance: func(context.Context, uuid.UUID) (entities.ProcessInstance, error) {
			return entities.ProcessInstance{}, wantErr
		},
	}
	handler := &EndEventHandler{engine: stub}

	instance := testInstance()
	instance.ParentInstance = &entities.ProcessInstance{ID: uuid.New()}
	instance.ParentNode = &entities.Node{ID: "check"}

	err := handler.DoExecute(t.Context(), instance, &entities.ProcessDefinition{}, entities.Node{ID: "end"}, "")

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the parent load failure to surface, got %v", err)
	}
}

// A parent instance with no definition reference used to be dereferenced
// straight through, panicking inside the engine.
func TestEndEventHandlerReportsParentWithoutDefinition(t *testing.T) {
	stub := &stubEngine{
		getInstance: func(_ context.Context, id uuid.UUID) (entities.ProcessInstance, error) {
			return entities.ProcessInstance{ID: id}, nil
		},
	}
	handler := &EndEventHandler{engine: stub}

	instance := testInstance()
	instance.ParentInstance = &entities.ProcessInstance{ID: uuid.New()}
	instance.ParentNode = &entities.Node{ID: "check"}

	err := handler.DoExecute(t.Context(), instance, &entities.ProcessDefinition{}, entities.Node{ID: "end"}, "")

	if err == nil {
		t.Fatal("a parent instance with no definition was accepted")
	}
	if !strings.Contains(err.Error(), "definition") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// The sub-process's own completion is persisted before the parent is touched, so
// a failure handing control back cannot also lose the fact that the child
// finished.
func TestEndEventHandlerPersistsCompletionBeforeResumingParent(t *testing.T) {
	stub := &stubEngine{
		proceedIteration: func(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string, string) error {
			return errors.New("parent resume failed")
		},
	}
	handler := &EndEventHandler{engine: stub}

	instance := testInstance()
	instance.ParentInstance = &entities.ProcessInstance{ID: uuid.New()}
	instance.ParentNode = &entities.Node{ID: "check"}

	if err := handler.DoExecute(t.Context(), instance, &entities.ProcessDefinition{}, entities.Node{ID: "end"}, ""); err == nil {
		t.Fatal("expected the parent resume failure to surface")
	}

	updateIdx, getParentIdx := -1, -1
	for i, call := range stub.calls {
		if call == "UpdateInstance" && updateIdx == -1 {
			updateIdx = i
		}
		if call == "GetInstance" && getParentIdx == -1 {
			getParentIdx = i
		}
	}
	if updateIdx == -1 {
		t.Fatalf("the completed sub-process was never persisted; calls were %v", stub.calls)
	}
	if getParentIdx != -1 && updateIdx > getParentIdx {
		t.Errorf("the parent was touched before the sub-process completion was persisted; calls were %v", stub.calls)
	}
	if instance.Status != entities.ProcessCompleted {
		t.Errorf("the sub-process was not marked completed, got status %v", instance.Status)
	}
}
