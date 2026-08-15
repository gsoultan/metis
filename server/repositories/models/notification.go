package models

type NotificationModel struct {
	Base
	UserID     string `gorm:"size:255;index" json:"user_id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Message    string `gorm:"type:text" json:"message"`
	IsRead     bool   `gorm:"default:false" json:"is_read"`
	Link       string `json:"link,omitzero"`
	ProjectID  *UUID  `gorm:"index" json:"project_id,omitzero"`
	InstanceID *UUID  `gorm:"index" json:"instance_id,omitzero"`
}

func (NotificationModel) TableName() string {
	return "notifications"
}
