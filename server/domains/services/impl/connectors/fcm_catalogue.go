package connectors

import (
	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// fcmConnectorID is fixed so the catalogue entry is the same row in every
// installation, like the other built-ins.
var fcmConnectorID = uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1b01")

// FCMCatalogueEntry is the Firebase Cloud Messaging entry on the Connectors page.
func FCMCatalogueEntry() entities.Connector {
	return entities.Connector{
		ID:          fcmConnectorID,
		Key:         FCMConnectorKey,
		Name:        "Firebase Cloud Messaging",
		Description: "Send push notifications to people's phones and browsers through your Firebase app",
		Icon:        "BellRing",
		Type:        "messaging",
		Schema: []entities.ConnectorProperty{
			{
				Key:         FCMServiceAccountKey,
				Label:       "Service account key",
				Type:        "secret-file",
				Description: "The JSON file from Firebase console: Project settings, Service accounts, Generate new private key. Stored encrypted; nobody can read it back.",
				Required:    true,
			},
			{
				Key:          FCMUseForPush,
				Label:        "Use for push in this project",
				Type:         "boolean",
				Description:  "Reminders and push templates send through this connection.",
				DefaultValue: "true",
			},
		},
	}
}
