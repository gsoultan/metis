package connectors

import (
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The fields a step fills in for the built-in connectors that send a message.
//
// Each of these connectors reads its message from the payload under names the
// step panel never showed — text, content, to, subject, token — so the only way
// to send anything was to know them and type them into an input mapping. Each
// is declared here once, under the payload name its executor reads, and the
// step stores it under StepFieldPrefix plus that name.
//
// None is required here. A step written before these fields existed sends its
// message through an input mapping or a variable of that name, and still does;
// marking the field required would flag every such step in the designer. The
// connector still refuses a message it was given nothing for.

// fieldTextarea is drawn as a box several lines high.
const fieldTextarea = "textarea"

// templateHint is what every message field says about writing one.
const templateHint = "Write {{variable}} to put in a process value, for example {{po_number}}."

func stepField(name, label, fieldType, description string) entities.ConnectorProperty {
	return entities.ConnectorProperty{
		Key:         servicecontracts.StepFieldPrefix + name,
		Label:       label,
		Type:        fieldType,
		Description: description,
	}
}

func chatFields(textName string) []entities.ConnectorProperty {
	return []entities.ConnectorProperty{
		stepField(textName, "Message", fieldTextarea, templateHint),
	}
}

// emailFields names the recipient and the text under the payload names the
// executor reads: SendGrid's reads to_email and content, the others to and
// body.
func emailFields(toName, bodyName string) []entities.ConnectorProperty {
	return []entities.ConnectorProperty{
		stepField(toName, "To", fieldString, "One address, or several separated by commas. "+templateHint),
		stepField("subject", "Subject", fieldString, templateHint),
		stepField(bodyName, "Message", fieldTextarea, templateHint),
	}
}

// StepFieldSchemas returns the step fields of each built-in connector that
// sends a message, by connector key.
func StepFieldSchemas() map[string][]entities.ConnectorProperty {
	return map[string][]entities.ConnectorProperty{
		SlackConnectorKey:   chatFields("text"),
		TeamsConnectorKey:   append([]entities.ConnectorProperty{stepField("title", "Title", fieldString, templateHint)}, chatFields("text")...),
		"discord-message":   chatFields("content"),
		EmailConnectorKey:   emailFields("to", "body"),
		PanmailConnectorKey: emailFields("to", "body"),
		"sendgrid-email":    emailFields("to_email", "content"),
		FCMConnectorKey: {
			stepField("token", "Device token", fieldString, "The device's registration token. "+templateHint),
			stepField("title", "Title", fieldString, templateHint),
			stepField("body", "Message", fieldTextarea, templateHint),
			stepField("link", "Link", fieldString, "Opened when the notification is tapped. "+templateHint),
		},
	}
}

// RenderStepText fills in a step field's {{ }} expressions from the process
// variables. A field that is one expression keeps its value's type.
func RenderStepText(template string, variables map[string]any) (any, error) {
	return renderString(template, variables)
}
