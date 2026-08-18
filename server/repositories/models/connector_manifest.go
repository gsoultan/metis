package models

// ConnectorManifestModel is a connector described by a document rather than by
// Go — see services/impl/connectors.Manifest.
//
// The document is the record. Its key, version and name are stored alongside so
// a catalogue can be listed without parsing every row, but they are derived: on
// every write they come from the document, and the document is what is executed.
// Two sources of truth for "which connector is this" is how a catalogue starts
// showing one thing and calling another.
type ConnectorManifestModel struct {
	Base

	// Key is what a node names, and is unique across the installation for the
	// same reason a built-in connector's key is: a node says "salesforce.create-lead"
	// and exactly one thing must answer.
	Key string `gorm:"size:191;uniqueIndex" json:"key"`

	Name    string `gorm:"size:255" json:"name"`
	Version int    `json:"version"`

	// Document is the manifest as its author wrote it, YAML or JSON. Stored
	// verbatim rather than as parsed fields so that what an operator reads back
	// is what they installed, comments and all.
	Document string `gorm:"type:text" json:"document"`

	// Enabled is the switch. A connector calling a partner that has asked us to
	// stop needs to be stoppable without deleting it, because deleting loses
	// the document.
	Enabled bool `gorm:"default:true" json:"enabled"`
}

// TableName overrides the table name for ConnectorManifestModel.
func (ConnectorManifestModel) TableName() string {
	return "connector_manifests"
}
