package models

type DeploymentModel struct {
	Base
	ProjectID UUID            `gorm:"index" json:"project_id,omitzero"`
	Name      string          `json:"name"`
	Resources []ResourceModel `gorm:"foreignKey:DeploymentID" json:"resources,omitzero"`
}

func (DeploymentModel) TableName() string {
	return "deployments"
}

type ResourceModel struct {
	Base
	DeploymentID UUID   `gorm:"index" json:"deployment_id,omitzero"`
	Name         string `json:"name"`
	Content      []byte `json:"content,omitzero"`
	Type         string `json:"type"`
}

func (ResourceModel) TableName() string {
	return "deployment_resources"
}
