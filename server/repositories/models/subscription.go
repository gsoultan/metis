package models

type SubscriptionType string

const (
	SubscriptionSignal  SubscriptionType = "signal"
	SubscriptionMessage SubscriptionType = "message"
)

type Subscription struct {
	Base
	ProjectID      UUID             `json:"project_id,omitzero"`
	InstanceID     UUID             `json:"instance_id,omitzero"`
	NodeID         string           `json:"node_id"`
	Type           SubscriptionType `json:"type"`
	EventName      string           `json:"event_name"`
	CorrelationKey string           `json:"correlation_key,omitzero"`
}

func (Subscription) TableName() string {
	return "event_subscriptions"
}
