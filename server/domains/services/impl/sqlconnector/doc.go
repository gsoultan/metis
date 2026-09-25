// Package sqlconnector is the database lookup: a process step that reads rows
// from another database into a process variable, so a decision can be made on
// data the process does not carry.
//
// # Who writes the query, and what that costs
//
// The query is written in the designer, by whoever designs the process, and a
// process definition is untrusted input (AGENTS.md §0). There is no sandbox
// for SQL the way there is for a script task, so the controls are these, and
// the residual risk is stated rather than hidden: anybody who can deploy a
// lookup can read whatever the connection's login can read, on any database an
// administrator has connected.
//
//   - Deploying a definition with a lookup in it needs the QUERY_AUTHOR role,
//     checked where every deploy path meets (definitionService.DeployDefinition).
//   - The connection — server, login, password — is an administrator's, stored
//     encrypted on the connector instance. A step names none of it; see
//     ConnectorRequest.
//   - Values from the process are only ever bound as parameters (bind.go).
//   - The query must be one statement that reads, checked in every way the
//     servers could read it (statement.go, lexer.go).
//   - PostgreSQL and MySQL run it in a read-only transaction the server
//     enforces, and are held to one statement per query by the protocol,
//     whatever the connection string asks for (postgres.go, mysql.go).
//   - SQL Server has no read-only transaction. There the statement check and
//     the login's own permissions are what stand in the way, and the
//     transaction every lookup runs in is rolled back, so a write that got past
//     both is undone — one with effects outside the database is not.
//   - A pool is not used if its login can see Metis's own tables
//     (own_database.go). On SQL Server that sees only the current database.
//   - A query is stopped by the server at its time limit, and its answer is
//     bounded in rows and bytes.
//   - An operator can hold lookups to a list of hosts (host_policy.go).
//
// The login is the boundary that matters. Give a lookup's connection a login
// that can SELECT the tables its lookups need and nothing else; everything
// here is a second line behind that one.
package sqlconnector
