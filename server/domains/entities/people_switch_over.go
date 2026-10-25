package entities

// DirectoryGap is a name a project's work uses that its People directory does
// not have: a person nobody added, or a group nobody made.
//
// It is what has to be filled before Users stop doing tasks. A User whose
// username a task names is doing that task today; once the switch-over window
// closes, nobody is.
type DirectoryGap struct {
	Name string `json:"name"`
	// Kind is "person" for an assignee or a candidate user, "group" for a
	// candidate group.
	Kind string `json:"kind"`
	// OpenTasks counts the project's open tasks that name it.
	OpenTasks int `json:"open_tasks"`
	// Models are the keys of the project's live process models that name it.
	Models []string `json:"models,omitzero"`
	// HasAccount says a User of this name is in the project's organization, so
	// the person can be added from their account. DisplayName and Email are
	// the account's.
	HasAccount  bool   `json:"has_account,omitzero"`
	DisplayName string `json:"display_name,omitzero"`
	Email       string `json:"email,omitzero"`
	// HasPlatformGroup says the organization has a group of this name, and
	// Members are its members' usernames: who the People group would hold.
	HasPlatformGroup bool     `json:"has_platform_group,omitzero"`
	Members          []string `json:"members,omitzero"`
}

// DirectoryGapImport is what adding the gaps to the directory did.
type DirectoryGapImport struct {
	// People counts the people added from Users' accounts.
	People int `json:"people"`
	// Groups counts the People groups made from the organization's groups.
	Groups int `json:"groups"`
	// Skipped names what was asked for and not added, with why.
	Skipped []ImportProblem `json:"skipped,omitzero"`
}
