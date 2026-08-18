package entities

import (
	"github.com/google/uuid"
	"time"
)

type SubscriptionType string

const (
	SubscriptionSignal  SubscriptionType = "signal"
	SubscriptionMessage SubscriptionType = "message"
)

// EventSubscription represents a waiting event in a process instance.
type EventSubscription struct {
	ID             uuid.UUID        `json:"id"`
	Project        *Project         `json:"project,omitzero"`
	Instance       *ProcessInstance `json:"instance,omitzero"`
	Node           *Node            `json:"node,omitzero"`
	Type           SubscriptionType `json:"type"`
	EventName      string           `json:"event_name"`
	CorrelationKey string           `json:"correlation_key,omitzero"`
	CreatedAt      time.Time        `json:"created_at"`
}

func NewSignalSubscription(project *Project, instance *ProcessInstance, node *Node, signalName string) EventSubscription {
	return EventSubscription{
		ID:        mustID(),
		Project:   project,
		Instance:  instance,
		Node:      node,
		Type:      SubscriptionSignal,
		EventName: signalName,
		CreatedAt: time.Now(),
	}
}

func NewMessageSubscription(project *Project, instance *ProcessInstance, node *Node, messageName, correlationKey string) EventSubscription {
	return EventSubscription{
		ID:             mustID(),
		Project:        project,
		Instance:       instance,
		Node:           node,
		Type:           SubscriptionMessage,
		EventName:      messageName,
		CorrelationKey: correlationKey,
		CreatedAt:      time.Now(),
	}
}
