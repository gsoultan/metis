package sqlconnector

import (
	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// catalogueID is the lookup's row in the connector catalogue, fixed like every
// other built-in's so it is the same row on every installation.
var catalogueID = uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a21")

// The field types the designer draws for a step's lookup.
const (
	fieldTextarea = "textarea"
	fieldMapping  = "mapping"
	fieldString   = "string"
)

// CatalogueEntry is the database lookup as the connector catalogue lists it,
// with the settings an administrator fills in once for a project.
func CatalogueEntry() entities.Connector {
	return entities.Connector{
		ID:          catalogueID,
		Key:         Key,
		Name:        "Database Lookup",
		Description: "Look something up in your own database — a customer's tier, an account's balance — to decide on",
		Icon:        "Database",
		Type:        "data",
		Schema: []entities.ConnectorProperty{
			{Key: driverSetting, Label: "Database", Type: "select", Required: true, DefaultValue: driverPostgres,
				Options: []any{driverPostgres, driverMySQL, driverSQLServer}},
			{Key: dsnSetting, Label: "Connection string", Type: "password", Required: true,
				Description: "Connect as a login that can read only the tables your lookups need. " +
					"It is the boundary that matters: the queries are written by whoever designs the process. " +
					"On SQL Server it is the only one that stops a write, since SQL Server cannot run a read-only transaction."},
			{Key: timeoutSetting, Label: "Time limit (milliseconds)", Type: "number", DefaultValue: "5000",
				Description: "A lookup running longer is stopped by the database. At most 30000."},
			{Key: maxRowsSetting, Label: "Most rows", Type: "number", DefaultValue: "500",
				Description: "A lookup stops reading here and says its answer is incomplete. At most 10000."},
			{Key: maxResultBytesSetting, Label: "Most data (bytes)", Type: "number", DefaultValue: "262144",
				Description: "The same, by size: the answer is stored with the process. At most 1048576."},
		},
	}
}

// NodeSchema is what a step using the lookup fills in.
func NodeSchema() []entities.ConnectorProperty {
	return []entities.ConnectorProperty{
		{Key: servicecontracts.StatementProperty, Label: "Query", Type: fieldTextarea, Required: true,
			Description: "One SELECT. Write each value from the process as :name — " +
				"SELECT tier FROM customers WHERE id = :customer_id — and say where it comes from below."},
		{Key: servicecontracts.ParamsProperty, Label: "Values", Type: fieldMapping,
			Description: "For each :name in the query, the process value it takes."},
		{Key: servicecontracts.ResultVariableProperty, Label: "Store the answer as", Type: fieldString, Required: true,
			Description: "A name like customer. The first row is customer.row, every row is customer.rows, " +
				"and customer.row_count is how many were found."},
	}
}
