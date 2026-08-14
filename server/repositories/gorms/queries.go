package gorms

// ByKey builds a condition on the `key` column.
//
// It has to be a map rather than a raw "key = ?" string: `key` is a reserved
// word in MySQL, and a raw condition is passed through verbatim, so MySQL
// rejected the statement outright. GORM quotes map keys per dialect — backticks
// on MySQL, double quotes on PostgreSQL — which is the only form all three
// engines accept.
func ByKey(key string) map[string]any {
	return map[string]any{"key": key}
}

// ByKeyAndVersion is ByKey with an explicit version.
func ByKeyAndVersion(key string, version int) map[string]any {
	return map[string]any{"key": key, "version": version}
}

// OrderLatestDefinition defines the sorting for versions.
const OrderLatestDefinition = "version desc"

// QueryByID is used for finding a record by its primary ID.
const QueryByID = "id = ?"

// QueryByProjectID is used for filtering records by project.
const QueryByProjectID = "project_id = ?"

// UpdateFieldStatus is the column name for status updates.
const UpdateFieldStatus = "status"

// QueryByAssignee is used to filter tasks by their assignee.
const QueryByAssignee = "assignee = ?"

// QueryByInstanceID is used to filter records by process instance ID.
const QueryByInstanceID = "instance_id = ?"

// QueryByStatus is used to filter records by their current status.
const QueryByStatus = "status = ?"

// QueryByCandidateUser is used to check for candidate user in a JSON-stored list.
const QueryByCandidateUser = "candidate_users LIKE ?"

// QueryByCandidateGroup is used to check for candidate group in a JSON-stored list.
const QueryByCandidateGroup = "candidate_groups LIKE ?"

// QueryByDefinitionID is used to filter by definition ID.
const QueryByDefinitionID = "definition_id = ?"

// QueryByPriority is used to filter by task priority.
const QueryByPriority = "priority = ?"

// QueryByUsername is used to filter by user's username.
const QueryByUsername = "username = ?"

// QueryByOrganizationID is used to filter by organization ID.
const QueryByOrganizationID = "organization_id = ?"

// QueryTenantScopeViaProject is the JOIN clause used to scope list queries by
// tenant (organization) through the projects table. Apply when a TenantContext
// is present in the request context.
const QueryTenantScopeViaProject = "JOIN projects ON projects.id = {table}.project_id AND projects.organization_id = ?"
