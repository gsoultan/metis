package connectors

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/entities"
)

func settingKeys(settings []entities.ConnectorProperty) []string {
	keys := make([]string, len(settings))
	for i, setting := range settings {
		keys[i] = setting.Key
	}
	return keys
}

func settingNamed(t *testing.T, settings []entities.ConnectorProperty, key string) entities.ConnectorProperty {
	t.Helper()
	for _, setting := range settings {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("the form does not ask for %q: %v", key, settingKeys(settings))
	return entities.ConnectorProperty{}
}

// A connection has to hold whatever the manifest's authentication reads, or no
// call it makes can be signed.
func TestTheConnectionFormAsksForWhatTheAuthenticationReads(t *testing.T) {
	cases := map[string]struct {
		auth     string
		want     []string
		required []string
	}{
		"none":         {auth: "none"},
		"unspecified":  {auth: ""},
		"basic":        {auth: "basic", want: []string{"username", "password"}, required: []string{"username"}},
		"bearer":       {auth: "bearer", want: []string{"token"}, required: []string{"token"}},
		"api key":      {auth: "api_key", want: []string{"api_key"}, required: []string{"api_key"}},
		"client creds": {auth: "oauth2_client_credentials", want: []string{"client_id", "client_secret", "audience", "scope"}, required: []string{"client_id", "client_secret"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := Manifest{Key: "x", Auth: Auth{Type: tc.auth, TokenURL: "https://login.example.com/token"},
				Request: Request{URL: "https://api.example.com"}}
			settings := manifest.CatalogueEntry().Schema
			if got := settingKeys(settings); !slices.Equal(got, tc.want) {
				t.Errorf("asks for %v, want %v", got, tc.want)
			}
			for _, key := range tc.required {
				if !settingNamed(t, settings, key).Required {
					t.Errorf("%q is not marked required", key)
				}
			}
		})
	}
}

// The field names above are only right if they are the ones the call reads. A
// connection filled in exactly as the form asks has to authenticate.
func TestAConnectionFilledInAsTheFormAsksAuthenticates(t *testing.T) {
	for _, authType := range []string{authNone, authBasic, authBearer, authAPIKey, authOAuth2ClientCredentials} {
		t.Run(authType, func(t *testing.T) {
			auth := Auth{Type: authType, TokenURL: "https://login.example.com/token"}
			manifest := Manifest{Key: "x", Auth: auth, Request: Request{URL: "https://api.example.com"}}
			config := map[string]any{}
			for _, setting := range manifest.CatalogueEntry().Schema {
				if setting.Required {
					config[setting.Key] = "filled-in"
				}
			}

			// OAuth fetches a token over the network; what the connection
			// supplies is decided before that, by credentialsFrom.
			if authType == authOAuth2ClientCredentials {
				if _, err := credentialsFrom(auth, config); err != nil {
					t.Fatalf("a connection filled in as asked cannot authenticate: %v", err)
				}
				return
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example.com", http.NoBody)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if err := applyAuth(t.Context(), request, auth, config, nil); err != nil {
				t.Fatalf("a connection filled in as asked cannot authenticate: %v", err)
			}
		})
	}
}

// config_schema is where an author says what a connection holds, so the form
// is drawn from it: a title, a description, a type, a default, a choice.
func TestTheConnectionFormIsDrawnFromConfigSchema(t *testing.T) {
	manifest := parse(t, `
key: x
config_schema:
  type: object
  required: [instance_url]
  properties:
    instance_url: {type: string, format: uri, title: Salesforce address, description: Where your org lives}
    region: {type: string, enum: [eu, us], default: eu}
    timeout: {type: integer}
    sandbox: {type: boolean, default: false}
    pin: {type: string, format: password}
request:
  url: "{{config.instance_url}}/leads"
`)
	settings := manifest.CatalogueEntry().Schema

	if got := settingKeys(settings); !reflect.DeepEqual(got, []string{"instance_url", "pin", "region", "sandbox", "timeout"}) {
		t.Fatalf("asks for %v", got)
	}
	address := settingNamed(t, settings, "instance_url")
	if address.Label != "Salesforce address" || address.Description != "Where your org lives" || !address.Required || address.Type != fieldString {
		t.Errorf("instance_url = %+v", address)
	}
	region := settingNamed(t, settings, "region")
	if region.Type != fieldSelect || !reflect.DeepEqual(region.Options, []any{"eu", "us"}) || region.DefaultValue != "eu" || region.Label != "region" {
		t.Errorf("region = %+v", region)
	}
	if got := settingNamed(t, settings, "timeout").Type; got != fieldNumber {
		t.Errorf("timeout is a %q field", got)
	}
	if sandbox := settingNamed(t, settings, "sandbox"); sandbox.Type != fieldBoolean || sandbox.DefaultValue != "false" {
		t.Errorf("sandbox = %+v", sandbox)
	}
	// The list of a project's connections hides a setting by its name, and
	// "pin" is not one it hides. A password box would hide it on the screen
	// only, so the form does not pretend.
	if got := settingNamed(t, settings, "pin").Type; got != fieldString {
		t.Errorf("a setting the server does not keep from the browser is drawn as a %q field", got)
	}
}

// A password box has to mean the value is kept from the browser, and the server
// decides that by the setting's name. Every field the form draws as secret is
// one the server masks.
func TestEverySecretFieldIsOneTheServerKeepsFromTheBrowser(t *testing.T) {
	for _, authType := range []string{authBasic, authBearer, authAPIKey, authOAuth2ClientCredentials} {
		for _, setting := range authSettings(authType) {
			if setting.Type == fieldSecret && !configsecret.IsSensitive(setting.Key) {
				t.Errorf("%s: %q is drawn as a secret and returned to the browser in clear", authType, setting.Key)
			}
		}
	}
}

// A manifest that reads a setting without declaring it still needs the form to
// ask for it — the documentation's own example declares nothing, and every
// imported one reads {{config.base_url}}.
func TestTheConnectionFormAsksForWhatATemplateReadsWithoutDeclaringIt(t *testing.T) {
	manifest := parse(t, `
key: x
auth: {type: bearer}
config_schema:
  properties:
    region: {type: string}
request:
  url: "{{config.base_url}}/{{config.region}}/leads"
  headers:
    X-Tenant: "{{config.tenant}}"
    X-Trace: "{{input.config.trace}}"
  query:
    source: "https://config.example.com/{{input.source}}"
  body:
    owner:
      id: "{{if config.owner_id = null then 0 else config.owner_id}}"
    tags: ["{{config.tag}}", "plain"]
`)
	settings := manifest.CatalogueEntry().Schema

	want := []string{"token", "region", "base_url", "owner_id", "tag", "tenant"}
	if got := settingKeys(settings); !reflect.DeepEqual(got, want) {
		t.Fatalf("asks for %v, want %v", got, want)
	}
	if !settingNamed(t, settings, "base_url").Required {
		t.Error("base_url is read by the URL, so the call cannot be made without it, and it is not required")
	}
	if settingNamed(t, settings, "tenant").Required {
		t.Error("a header whose template finds nothing is left off, so tenant is not required")
	}
	if settingNamed(t, settings, "region").Required {
		t.Error("region is declared, and its declaration does not require it")
	}
}

func TestACatalogueEntryIsNamedAndFiledAsTheManifestSays(t *testing.T) {
	named := parse(t, "key: crm.lead\nname: Create a lead\ncategory: crm\nicon: Users\nrequest:\n  url: https://api.example.com\n")
	if entry := named.CatalogueEntry(); entry.Key != "crm.lead" || entry.Name != "Create a lead" || entry.Type != "crm" || entry.Icon != "Users" {
		t.Errorf("entry = %+v", entry)
	}

	unnamed := parse(t, "key: crm.lead\nrequest:\n  url: https://api.example.com\n")
	if entry := unnamed.CatalogueEntry(); entry.Name != "crm.lead" || entry.Type != defaultCatalogueType {
		t.Errorf("a manifest with no name or category is offered as %q in %q", entry.Name, entry.Type)
	}

	long := Manifest{Key: "x", Category: strings.Repeat("é", maxCatalogueTypeLength+10), Request: Request{URL: "https://e.com"}}
	if got := long.CatalogueEntry().Type; got != strings.Repeat("é", maxCatalogueTypeLength) {
		t.Errorf("a long category was filed as %q", got)
	}
}
