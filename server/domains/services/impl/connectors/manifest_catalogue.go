package connectors

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The catalogue entry a manifest brings with it.
//
// A process step does not name a manifest. It names a catalogue entry: the
// designer offers the entries, a project's connection configures one of them,
// and at run time the step's entry is resolved to that connection and the
// entry's key is what finds the manifest. So an installed manifest needs an
// entry to be chosen, connected or reached at all — and the entry's form has to
// ask for everything the manifest reads from the connection, or there is no way
// to give it a credential.

// defaultCatalogueType files a manifest that names no category beside the HTTP
// connector, which is what it most resembles.
const defaultCatalogueType = "utility"

// maxCatalogueTypeLength is the most characters the catalogue holds for a
// category. An imported operation takes its category from the API's first tag,
// which can be longer, and cutting it beats refusing the install.
const maxCatalogueTypeLength = 64

// The kinds of field the Connectors page draws for a connection.
const (
	fieldString  = "string"
	fieldNumber  = "number"
	fieldBoolean = "boolean"
	fieldSelect  = "select"
	// fieldSecret is drawn as a password box and never shown again once saved.
	fieldSecret = "password"
)

// CatalogueEntry is the entry that offers this manifest. It carries no id:
// whether it is a new entry or the one the key had before is the catalogue's
// to decide.
func (m Manifest) CatalogueEntry() entities.Connector {
	return entities.Connector{
		Key:    m.Key,
		Name:   cmp.Or(strings.TrimSpace(m.Name), m.Key),
		Icon:   m.Icon,
		Type:   catalogueType(m.Category),
		Schema: m.connectionSettings(),
	}
}

// connectionSettings are the fields a connection to this connector holds: what
// its authentication reads, then what config_schema declares, then whatever
// else a template reads from config without declaring it. A manifest written
// from the documentation's own example declares nothing, and an imported one
// reads {{config.base_url}}; both still need the form to ask.
func (m Manifest) connectionSettings() []entities.ConnectorProperty {
	settings := authSettings(m.Auth.Type)
	asked := make(map[string]bool)
	for _, setting := range settings {
		asked[setting.Key] = true
	}
	add := func(setting entities.ConnectorProperty) {
		if !asked[setting.Key] {
			asked[setting.Key] = true
			settings = append(settings, setting)
		}
	}

	for _, setting := range declaredSettings(m.ConfigSchema) {
		add(setting)
	}
	// A setting the URL reads is one the call cannot be made without.
	for _, name := range configReferences(m.Request.URL) {
		add(entities.ConnectorProperty{Key: name, Label: name, Type: fieldString, Required: true})
	}
	for _, name := range configReferences(m.Request.otherTemplates()...) {
		add(entities.ConnectorProperty{Key: name, Label: name, Type: fieldString})
	}
	return settings
}

// authSettings are the settings each kind of authentication reads — the names
// applyAuth and credentialsFrom look up.
func authSettings(authType string) []entities.ConnectorProperty {
	switch authType {
	case authBasic:
		// No password is required: some APIs take a key as the username and
		// nothing after it.
		return []entities.ConnectorProperty{
			{Key: "username", Label: "Username", Type: fieldString, Required: true},
			{Key: "password", Label: "Password", Type: fieldSecret},
		}
	case authBearer:
		return []entities.ConnectorProperty{{Key: "token", Label: "Token", Type: fieldSecret, Required: true}}
	case authAPIKey:
		return []entities.ConnectorProperty{{Key: "api_key", Label: "API key", Type: fieldSecret, Required: true}}
	case authOAuth2ClientCredentials:
		return []entities.ConnectorProperty{
			{Key: "client_id", Label: "Client ID", Type: fieldString, Required: true},
			{Key: "client_secret", Label: "Client secret", Type: fieldSecret, Required: true},
			{Key: "audience", Label: "Audience", Type: fieldString,
				Description: "Only for a provider that asks for one."},
			{Key: "scope", Label: "Scope", Type: fieldString,
				Description: "Narrows what this connection may do. Leave it empty for everything the connector asks for."},
		}
	default:
		return nil
	}
}

// declaredSettings reads config_schema's properties, in name order: a schema
// arrives as a map, so there is no order of the author's to keep.
func declaredSettings(schema map[string]any) []entities.ConnectorProperty {
	properties := propertiesOf(schema)
	required := requiredOf(schema)

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	slices.Sort(names)

	settings := make([]entities.ConnectorProperty, 0, len(names))
	for _, name := range names {
		property, isMap := properties[name].(map[string]any)
		if !isMap {
			property = map[string]any{}
		}
		settings = append(settings, settingFrom(name, property, required[name]))
	}
	return settings
}

// settingFrom turns one JSON Schema property into a field.
func settingFrom(name string, property map[string]any, required bool) entities.ConnectorProperty {
	setting := entities.ConnectorProperty{
		Key:         name,
		Label:       cmp.Or(textOf(property["title"]), name),
		Type:        fieldFor(property),
		Description: textOf(property["description"]),
		Required:    required,
	}
	if value, has := property["default"]; has && value != nil {
		setting.DefaultValue = fmt.Sprint(value)
	}
	if options, isList := property["enum"].([]any); isList && len(options) > 0 {
		setting.Type = fieldSelect
		setting.Options = options
	}
	return setting
}

// fieldFor is the field a property is drawn as.
//
// JSON Schema's `format: password` and `writeOnly` are deliberately not read.
// Whether a stored setting reaches the browser is decided by its name
// (configsecret.IsSensitive), and the form hides a value by the same rule. A
// password box for a setting whose name is not one of those would hide it on
// the screen while the list of a project's connections still returned it to
// every member — the very trap configsecret describes. So a setting that holds
// a credential says so in its name: secret, password, token, key.
func fieldFor(property map[string]any) string {
	switch textOf(property["type"]) {
	case "integer", "number":
		return fieldNumber
	case "boolean":
		return fieldBoolean
	default:
		return fieldString
	}
}

func requiredOf(schema map[string]any) map[string]bool {
	required := map[string]bool{}
	list, isList := schema["required"].([]any)
	if !isList {
		return required
	}
	for _, item := range list {
		if name, isText := item.(string); isText {
			required[name] = true
		}
	}
	return required
}

func textOf(value any) string {
	text, isText := value.(string)
	if !isText {
		return ""
	}
	return strings.TrimSpace(text)
}

// configReference finds `config.name` in an expression. What comes before it
// must not continue a name or a path, so neither `input.config.x` nor
// `myconfig.x` is taken for a setting.
var configReference = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])config\.([A-Za-z_][A-Za-z0-9_]*)`)

// configReferences are the settings the templates read, sorted and once each.
// Only what is inside the braces is an expression: the rest of a template is
// literal, and `https://config.example.com` reads nothing.
func configReferences(templates ...string) []string {
	var names []string
	for _, template := range templates {
		for _, expression := range expressionsIn(template) {
			for _, match := range configReference.FindAllStringSubmatch(expression, -1) {
				names = append(names, match[1])
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// expressionsIn returns what is inside each `{{ … }}` of a template, as far as
// renderString would read it.
func expressionsIn(template string) []string {
	var expressions []string
	rest := template
	for range maxTemplateExpressions {
		_, after, opened := strings.Cut(rest, openTag)
		if !opened {
			break
		}
		expression, remainder, closed := strings.Cut(after, closeTag)
		if !closed {
			break
		}
		expressions = append(expressions, expression)
		rest = remainder
	}
	return expressions
}

// otherTemplates are the request's templates other than its URL: header and
// query values, and every text in the body however deep.
func (r Request) otherTemplates() []string {
	var templates []string
	for _, value := range r.Headers {
		templates = append(templates, value)
	}
	for _, value := range r.Query {
		templates = append(templates, value)
	}
	return append(templates, textsIn(r.Body)...)
}

func textsIn(value any) []string {
	var texts []string
	switch v := value.(type) {
	case string:
		texts = append(texts, v)
	case map[string]any:
		for _, item := range v {
			texts = append(texts, textsIn(item)...)
		}
	case []any:
		for _, item := range v {
			texts = append(texts, textsIn(item)...)
		}
	}
	return texts
}

func catalogueType(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return defaultCatalogueType
	}
	if utf8.RuneCountInString(category) <= maxCatalogueTypeLength {
		return category
	}
	return string([]rune(category)[:maxCatalogueTypeLength])
}
