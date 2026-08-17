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

// QueryTenantScopeViaProjectOptional is the join half of the scope for tables
// whose project_id is nullable. It has to be a LEFT JOIN paired with
// QueryTenantScopeOptionalCondition: an inner join would silently drop every
// row that has no project, which for notifications means a system message
// addressed to a user would vanish from their inbox.
const QueryTenantScopeViaProjectOptional = "LEFT JOIN projects ON projects.id = {table}.project_id"

// QueryTenantScopeOptionalCondition is the predicate half of the nullable
// scope: the row belongs to the caller's organization, or it belongs to no
// project at all and is reachable only through the column that already
// identifies its owner.
const QueryTenantScopeOptionalCondition = "projects.organization_id = ? OR {table}.project_id IS NULL"

// QueryTenantScopeViaProjectSubquery scopes a table by project membership
// without joining, for statements where a JOIN is the wrong tool:
//
//   - SELECT ... FOR UPDATE, where joining projects would take row locks on a
//     second table on every call.
//   - UPDATE and DELETE, where join syntax is not portable across SQLite,
//     PostgreSQL and MySQL.
const QueryTenantScopeViaProjectSubquery = "{table}.project_id IN (SELECT id FROM projects WHERE organization_id = ?)"

// QueryTenantScopeViaProjectSubqueryOptional is the nullable counterpart of
// QueryTenantScopeViaProjectSubquery, for the same reason the LEFT JOIN variant
// exists: a notification that belongs to no project must stay reachable by the
// user it is addressed to.
const QueryTenantScopeViaProjectSubqueryOptional = "({table}.project_id IN (SELECT id FROM projects WHERE organization_id = ?) " +
	"OR {table}.project_id IS NULL)"

// QueryTenantScopeDirect scopes a table that carries organization_id itself and
// so needs no join to reach the tenant. Projects are the only such table: they
// are what every other scope joins *through*, which is exactly why nothing
// scoped them.
const QueryTenantScopeDirect = "{table}.organization_id = ?"

// QueryTenantScopeViaDeployment scopes deployment_resources, which carry no
// project_id of their own — their tenant is whichever project owns the parent
// deployment.
const QueryTenantScopeViaDeployment = "JOIN deployments ON deployments.id = deployment_resources.deployment_id " +
	"JOIN projects ON projects.id = deployments.project_id AND projects.organization_id = ?"

// QualifiedByID builds an unambiguous primary-key condition for a table.
//
// A bare "id = ?" is ambiguous the moment tenant scoping joins projects, which
// carries an id of its own — SQLite and Postgres reject the statement outright.
// Every read that can run under a tenant scope must qualify its columns.
func QualifiedByID(table string) string {
	return table + ".id = ?"
}
