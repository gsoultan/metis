package models

type SubscriptionType string

const (
	SubscriptionSignal  SubscriptionType = "signal"
	SubscriptionMessage SubscriptionType = "message"
)

type Subscription struct {
	Base
	ProjectID      UUID             `gorm:"index" json:"project_id,omitzero"`
	InstanceID     UUID             `gorm:"index" json:"instance_id,omitzero"`
	NodeID         string           `json:"node_id"`
	Type           SubscriptionType `gorm:"index" json:"type"`
	EventName      string           `gorm:"size:255;index" json:"event_name"`
	CorrelationKey string           `gorm:"size:512;index" json:"correlation_key,omitzero"`
}

func (Subscription) TableName() string {
	return "event_subscriptions"
}
