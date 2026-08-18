package connectors

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// A connector as data rather than as code.
//
// Every connector in this engine has been a Go switch branch: one function per
// vendor, in a file that only grows, and adding Salesforce means editing Go and
// redeploying. That does not scale past a handful and it locks out everyone who
// is not us.
//
// A manifest is the same thing written down — what to call, how to authenticate,
// what goes in, what comes back, and which failures are which. It can be listed,
// versioned, signed, shared and installed while the engine is running, because
// it is a document.
//
// This covers the roughly nine calls in ten that are "call a REST endpoint with
// auth". Connectors that are genuinely code-shaped — an SDK, a stream, anything
// stateful — keep the Go interface.

// Manifest is one connector.
type Manifest struct {
	Key     string `yaml:"key" json:"key"`
	Version int    `yaml:"version" json:"version"`
	Name    string `yaml:"name" json:"name"`

	Category string `yaml:"category,omitempty" json:"category,omitempty"`
	Icon     string `yaml:"icon,omitempty" json:"icon,omitempty"`

	Auth Auth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// ConfigSchema is what an administrator sets once, per tenant — the
	// instance URL, the account id. InputSchema is what a modeller sets per
	// node. Both are JSON Schema so the forms can draw themselves rather than
	// needing a screen written per connector.
	ConfigSchema map[string]any `yaml:"config_schema,omitempty" json:"config_schema,omitempty"`
	InputSchema  map[string]any `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
	OutputSchema map[string]any `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`

	Request  Request  `yaml:"request" json:"request"`
	Response Response `yaml:"response,omitempty" json:"response,omitempty"`

	// Errors turn an HTTP failure into a BPMN error code a boundary event can
	// catch, so an integration failing becomes a modelled business path rather
	// than a stack trace in an incident.
	Errors []ErrorRule `yaml:"errors,omitempty" json:"errors,omitempty"`
}

// Auth is how the call proves who it is.
type Auth struct {
	// Type is none, basic, bearer, api_key, or oauth2_client_credentials.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Header names where an api_key goes. Defaults to Authorization.
	Header string `yaml:"header,omitempty" json:"header,omitempty"`

	// TokenURL and Scopes are for the OAuth flows.
	TokenURL string   `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Scopes   []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// Request is the call to make.
//
// URL, headers and body values are templates: `{{config.instance_url}}` and
// `{{input.last_name}}` are substituted from the tenant's configuration and the
// node's inputs. The braces are the only syntax — everything inside them is
// FEEL, which is the same language conditions and mappings already use.
type Request struct {
	Method  string            `yaml:"method" json:"method"`
	URL     string            `yaml:"url" json:"url"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Query   map[string]string `yaml:"query,omitempty" json:"query,omitempty"`
	Body    map[string]any    `yaml:"body,omitempty" json:"body,omitempty"`
}

// Response says what success looks like and what to take from it.
type Response struct {
	// Success is a FEEL condition over `status`, `body` and `headers`. Empty
	// means any 2xx, which is what almost every API means.
	Success string `yaml:"success,omitempty" json:"success,omitempty"`

	// Outputs map a process variable to a FEEL expression over the response —
	// `body.id`, `headers['Location']`.
	Outputs map[string]string `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

// ErrorRule turns one kind of failure into a BPMN error.
type ErrorRule struct {
	// When is a FEEL condition over the same values Success sees.
	When string `yaml:"when" json:"when"`

	// BPMNError is the error code a boundary event catches.
	BPMNError string `yaml:"bpmn_error" json:"bpmn_error"`

	// Retryable says whether trying again could work. A 429 can; a 400 cannot,
	// and retrying it just spends the instance's attempts on the same answer.
	Retryable bool `yaml:"retryable,omitempty" json:"retryable,omitempty"`

	// RetryAfter is a FEEL expression giving how long to wait, usually read
	// from the response — `headers['Retry-After']`. Honouring what a partner
	// asks for is the difference between backing off and being blocked.
	RetryAfter string `yaml:"retry_after,omitempty" json:"retry_after,omitempty"`

	// Message overrides the text put on the incident.
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
}

// ParseManifest reads a manifest from YAML or JSON.
//
// YAML is what people write and JSON is what machines generate — an OpenAPI
// import, a catalogue download — and yaml.v3 reads both, JSON being a subset. So
// there is one parser rather than two that can disagree.
func ParseManifest(document []byte) (Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(document, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("connector manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate refuses a manifest that could not work.
//
// Checked at parse rather than at call time: a manifest is installed once and
// called thousands of times, and the moment to find out it names no URL is when
// somebody installs it, not when an instance reaches it at 3am.
func (m Manifest) Validate() error {
	var problems []string

	if strings.TrimSpace(m.Key) == "" {
		problems = append(problems, "it has no key, so no node could name it")
	}
	if strings.TrimSpace(m.Request.URL) == "" {
		problems = append(problems, "it has no request URL, so there is nothing to call")
	}
	if method := strings.ToUpper(strings.TrimSpace(m.Request.Method)); method != "" && !validMethods[method] {
		problems = append(problems, fmt.Sprintf("%q is not an HTTP method", m.Request.Method))
	}
	switch m.Auth.Type {
	case "", authNone, authBasic, authBearer, authAPIKey, authOAuth2ClientCredentials:
	default:
		problems = append(problems, fmt.Sprintf("%q is not a kind of authentication this understands", m.Auth.Type))
	}
	if m.Auth.Type == authOAuth2ClientCredentials && strings.TrimSpace(m.Auth.TokenURL) == "" {
		problems = append(problems, "it asks for OAuth but names no token URL")
	}
	for i, rule := range m.Errors {
		if strings.TrimSpace(rule.When) == "" {
			problems = append(problems, fmt.Sprintf("error rule %d says nothing about when it applies", i+1))
		}
		if strings.TrimSpace(rule.BPMNError) == "" {
			problems = append(problems, fmt.Sprintf("error rule %d names no BPMN error to raise", i+1))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("connector manifest %q cannot be used: %s", m.Key, strings.Join(problems, "; "))
	}
	return nil
}

// The authentication kinds a manifest may ask for.
const (
	authNone                    = "none"
	authBasic                   = "basic"
	authBearer                  = "bearer"
	authAPIKey                  = "api_key"
	authOAuth2ClientCredentials = "oauth2_client_credentials"
)

var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
}

// Method returns the HTTP method, defaulting to POST.
//
// POST rather than GET: a connector with a body is the common case, and a
// manifest that forgot to say is far more likely to be sending something than
// fetching it.
func (m Manifest) Method() string {
	if method := strings.ToUpper(strings.TrimSpace(m.Request.Method)); method != "" {
		return method
	}
	return "POST"
}
