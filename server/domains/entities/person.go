package entities

import "github.com/google/uuid"

// Person is somebody signed in from a project's People directory.
//
// Not a User and never mistaken for one: it is carried under its own context
// key, so a check that asks for the signed-in User finds nobody when a person
// is signed in, and the other way round. A person may act only in their own
// project and only on work a process gave them.
type Person struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitzero"`
	Email       string    `json:"email,omitzero"`
}

// PersonFrom is the signed-in view of a directory entry.
func PersonFrom(participant WorkflowUser) Person {
	person := Person{
		ID:          participant.ID,
		Username:    participant.Username,
		DisplayName: participant.DisplayName,
		Email:       participant.Email,
	}
	if participant.Project != nil {
		person.ProjectID = participant.Project.ID
	}
	return person
}
