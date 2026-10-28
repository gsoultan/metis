package models

type NotificationModel struct {
	Base
	UserID     string `json:"user_id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	IsRead     bool   `json:"is_read"`
	Link       string `json:"link,omitzero"`
	ProjectID  *UUID  `json:"project_id,omitzero"`
	InstanceID *UUID  `json:"instance_id,omitzero"`
}

func (NotificationModel) TableName() string {
	return "notifications"
}
