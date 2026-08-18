package connectors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Turning somebody else's API description into connectors.
//
// "Does this integrate with X?" has been a roadmap question. Almost every API
// worth integrating with publishes an OpenAPI document, and a manifest is close
// enough to one operation in that document that the translation is mechanical:
// the path becomes a URL template, the parameters become inputs, the request
// body becomes a body template, the response schema becomes outputs.
//
// So the answer becomes "import their spec". What comes out is a starting point
// rather than a finished connector — an author will rename things and delete the
// nine operations in ten they do not want — but a starting point that already
// calls the right endpoint with the right shape is a different proposition from
// an empty file.

// openAPIDocument is the subset of OpenAPI 3 this reads.
//
// Deliberately partial: a full parser is a large dependency and most of what it
// would parse has no bearing on making a call. What is here is what an operation
// needs to be callable, and anything unrecognised is ignored rather than
// refused, because a document with one exotic corner should still yield the
// other forty operations.
type openAPIDocument struct {
	OpenAPI string `yaml:"openapi" json:"openapi"`
	Info    struct {
		Title   string `yaml:"title" json:"title"`
		Version string `yaml:"version" json:"version"`
	} `yaml:"info" json:"info"`

	Servers []struct {
		URL string `yaml:"url" json:"url"`
	} `yaml:"servers" json:"servers"`

	Paths map[string]map[string]openAPIOperation `yaml:"paths" json:"paths"`

	Components struct {
		SecuritySchemes map[string]openAPISecurityScheme `yaml:"securitySchemes" json:"securitySchemes"`
	} `yaml:"components" json:"components"`
}

type openAPIOperation struct {
	OperationID string   `yaml:"operationId" json:"operationId"`
	Summary     string   `yaml:"summary" json:"summary"`
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags" json:"tags"`

	Parameters []openAPIParameter `yaml:"parameters" json:"parameters"`

	RequestBody struct {
		Required bool                        `yaml:"required" json:"required"`
		Content  map[string]openAPIMediaType `yaml:"content" json:"content"`
	} `yaml:"requestBody" json:"requestBody"`

	Responses map[string]struct {
		Description string                      `yaml:"description" json:"description"`
		Content     map[string]openAPIMediaType `yaml:"content" json:"content"`
	} `yaml:"responses" json:"responses"`
}

type openAPIParameter struct {
	Name        string         `yaml:"name" json:"name"`
	In          string         `yaml:"in" json:"in"`
	Required    bool           `yaml:"required" json:"required"`
	Description string         `yaml:"description" json:"description"`
	Schema      map[string]any `yaml:"schema" json:"schema"`
}

type openAPIMediaType struct {
	Schema map[string]any `yaml:"schema" json:"schema"`
}

type openAPISecurityScheme struct {
	Type   string `yaml:"type" json:"type"`
	Scheme string `yaml:"scheme" json:"scheme"`
	In     string `yaml:"in" json:"in"`
	Name   string `yaml:"name" json:"name"`
}

// ImportOpenAPI turns a specification into one manifest per operation.
//
// The manifests are returned in a stable order — by path, then by method —
// because a list that reshuffles on every import is one nobody can diff.
func ImportOpenAPI(document []byte) ([]Manifest, error) {
	var spec openAPIDocument
	if err := yaml.Unmarshal(document, &spec); err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}
	if len(spec.Paths) == 0 {
		return nil, fmt.Errorf("openapi: the document describes no paths, so there is nothing to import")
	}

	auth := authFrom(spec)
	base := baseURL(spec)

	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var manifests []Manifest
	for _, path := range paths {
		operations := spec.Paths[path]

		methods := make([]string, 0, len(operations))
		for method := range operations {
			if validMethods[strings.ToUpper(method)] {
				methods = append(methods, method)
			}
		}
		sort.Strings(methods)

		for _, method := range methods {
			manifest := manifestFor(spec, path, method, operations[method], base, auth)
			// A generated manifest still has to be one that could work; a
			// document with a broken operation should not produce a connector
			// that fails the first time somebody uses it.
			if err := manifest.Validate(); err != nil {
				continue
			}
			manifests = append(manifests, manifest)
		}
	}

	if len(manifests) == 0 {
		return nil, fmt.Errorf("openapi: no operation in the document could be turned into a connector")
	}
	return manifests, nil
}

// pathParameter matches the {name} placeholders OpenAPI puts in a path.
var pathParameter = regexp.MustCompile(`\{([^}]+)\}`)

func manifestFor(
	spec openAPIDocument,
	path, method string,
	operation openAPIOperation,
	base string,
	auth Auth,
) Manifest {
	// The path's own placeholders become input templates, so `/pets/{petId}`
	// calls `{{input.petId}}` — which is what the caller of the connector
	// supplies, and the same name the spec already uses.
	url := base + pathParameter.ReplaceAllString(path, "{{input.$1}}")

	query := map[string]string{}
	headers := map[string]string{}
	properties := map[string]any{}
	var required []string

	for _, parameter := range operation.Parameters {
		if parameter.Name == "" {
			continue
		}
		template := "{{input." + parameter.Name + "}}"
		switch parameter.In {
		case "query":
			query[parameter.Name] = template
		case "header":
			headers[parameter.Name] = template
		}
		properties[parameter.Name] = schemaFor(parameter)
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}

	body, bodyProperties, bodyRequired := bodyFrom(operation)
	for name, schema := range bodyProperties {
		properties[name] = schema
	}
	required = append(required, bodyRequired...)
	sort.Strings(required)

	inputSchema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	manifest := Manifest{
		Key:         keyFor(spec, path, method, operation),
		Version:     1,
		Name:        nameFor(path, method, operation),
		Category:    categoryFor(operation),
		Auth:        auth,
		InputSchema: inputSchema,
		Request: Request{
			Method:  strings.ToUpper(method),
			URL:     url,
			Query:   nonEmpty(query),
			Headers: nonEmpty(headers),
			Body:    body,
		},
		Response: Response{
			Outputs: outputsFrom(operation),
		},
		// Every API fails in these two ways, and a boundary event that can catch
		// them is worth more than a generated connector that treats every
		// failure alike. An author is free to delete or extend them.
		Errors: []ErrorRule{
			{When: "status = 401 or status = 403", BPMNError: "AUTH_FAILED", Retryable: false},
			{When: "status = 429", BPMNError: "RATE_LIMITED", Retryable: true, RetryAfter: "headers['Retry-After']"},
		},
	}

	if schema := responseSchema(operation); schema != nil {
		manifest.OutputSchema = schema
	}
	return manifest
}

// bodyFrom turns a request body's schema into a body template.
//
// One level of properties, mapped straight across. Nested objects are left to
// the author: guessing at how a caller would supply a nested shape produces a
// template that looks right and is wrong, and the schema is on the manifest for
// them to read.
func bodyFrom(operation openAPIOperation) (map[string]any, map[string]any, []string) {
	media, found := operation.RequestBody.Content["application/json"]
	if !found {
		return nil, nil, nil
	}

	properties := propertiesOf(media.Schema)
	if len(properties) == 0 {
		return nil, nil, nil
	}

	body := make(map[string]any, len(properties))
	inputs := make(map[string]any, len(properties))
	for name, schema := range properties {
		body[name] = "{{input." + name + "}}"
		inputs[name] = schema
	}

	var required []string
	if list, isList := media.Schema["required"].([]any); isList {
		for _, item := range list {
			if name, isText := item.(string); isText {
				required = append(required, name)
			}
		}
	}
	return body, inputs, required
}

// outputsFrom maps the success response's top-level fields to process variables.
func outputsFrom(operation openAPIOperation) map[string]string {
	schema := responseSchema(operation)
	if schema == nil {
		return nil
	}
	properties := propertiesOf(schema)
	if len(properties) == 0 {
		return nil
	}

	outputs := make(map[string]string, len(properties))
	for name := range properties {
		outputs[name] = "body." + name
	}
	return outputs
}

// responseSchema finds the schema of the first successful response.
func responseSchema(operation openAPIOperation) map[string]any {
	for _, status := range []string{"200", "201", "202", "default"} {
		response, found := operation.Responses[status]
		if !found {
			continue
		}
		if media, hasJSON := response.Content["application/json"]; hasJSON && media.Schema != nil {
			return media.Schema
		}
	}
	return nil
}

// authFrom reads the document's first security scheme.
//
// First rather than all: a manifest signs a call one way, and an API offering a
// choice is one where any of them works. An author who needs a different one
// changes a line.
func authFrom(spec openAPIDocument) Auth {
	names := make([]string, 0, len(spec.Components.SecuritySchemes))
	for name := range spec.Components.SecuritySchemes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		scheme := spec.Components.SecuritySchemes[name]
		switch {
		case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer"):
			return Auth{Type: authBearer}
		case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "basic"):
			return Auth{Type: authBasic}
		case scheme.Type == "apiKey" && scheme.In == "header":
			return Auth{Type: authAPIKey, Header: scheme.Name}
		}
	}
	return Auth{Type: authNone}
}

// baseURL is where the calls go.
//
// A document's server URL is often a template of its own — `{region}.api.com` —
// or absent entirely. Either way the base becomes configuration, because the
// same spec is used against production and a sandbox and the difference is not
// something to bake into a connector.
func baseURL(spec openAPIDocument) string {
	if len(spec.Servers) > 0 && strings.Contains(spec.Servers[0].URL, "://") && !strings.Contains(spec.Servers[0].URL, "{") {
		return strings.TrimRight(spec.Servers[0].URL, "/")
	}
	return "{{config.base_url}}"
}

// keyFor names the connector.
//
// An operationId when the document gives one — it is what the API's own
// documentation calls the operation, so it is what somebody will look for. The
// method and path otherwise, which is ugly and unambiguous.
func keyFor(spec openAPIDocument, path, method string, operation openAPIOperation) string {
	prefix := slug(spec.Info.Title)
	if prefix == "" {
		prefix = "imported"
	}
	if operation.OperationID != "" {
		return prefix + "." + slug(operation.OperationID)
	}
	return prefix + "." + strings.ToLower(method) + slug(path)
}

func nameFor(path, method string, operation openAPIOperation) string {
	if operation.Summary != "" {
		return operation.Summary
	}
	return strings.ToUpper(method) + " " + path
}

func categoryFor(operation openAPIOperation) string {
	if len(operation.Tags) > 0 {
		return operation.Tags[0]
	}
	return ""
}

func schemaFor(parameter openAPIParameter) map[string]any {
	schema := parameter.Schema
	if schema == nil {
		schema = map[string]any{"type": "string"}
	}
	if parameter.Description != "" {
		// Copied so the document's own schema map is not modified, which would
		// leak between the operations that share it.
		copied := make(map[string]any, len(schema)+1)
		for k, v := range schema {
			copied[k] = v
		}
		copied["description"] = parameter.Description
		return copied
	}
	return schema
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(text string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(text), "-"), "-")
}

func nonEmpty(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return in
}

// propertiesOf reads a schema's properties, if it has any that look like
// properties. A schema that names none — a bare string, an array, a $ref this
// does not follow — yields nothing rather than an error: one exotic operation
// should not stop the other forty being imported.
func propertiesOf(schema map[string]any) map[string]any {
	properties, isMap := schema["properties"].(map[string]any)
	if !isMap {
		return nil
	}
	return properties
}
