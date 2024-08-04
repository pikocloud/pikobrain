package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/invopop/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"

	"github.com/pikocloud/pikobrain/internal/utils"
)

// Parse remote document (schema).
func parseRemote(ctx context.Context, link string) (*schemaDocument, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	if u.Scheme == "file" {
		file := path.Join(u.Hostname(), u.Path)
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("open file %q: %w", file, err)
		}
		defer f.Close()
		return parseDocument(f)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return parseDocument(resp.Body)
}

// Parse schema and adapt it for tooling.
func parseDocument(src io.Reader) (*schemaDocument, error) {
	var doc schemaDocument
	err := yaml.NewDecoder(src).Decode(&doc)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	doc.convertRefs()
	doc.appendParams()
	doc.linkDoc()
	return &doc, nil
}

// Modified representation of OpenAPI schema.
type schemaDocument struct {
	Paths      map[string]*schemaPath `json:"paths"`
	Components struct {
		Schemas map[string]*schemaObject `json:"schemas"`
	} `json:"components"`
	Servers []struct {
		URL string `json:"url" yaml:"url"`
	} `json:"servers,omitempty"`
	cache map[string]*jsonschema.Schema
}

// object that assistant will use to generate request to tool.
type toolRequest struct {
	Path    map[string]any  `json:"path,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
	Query   map[string]any  `json:"query,omitempty"`
	Headers map[string]any  `json:"header,omitempty"`
}

type bodyContent struct {
	Schema *schemaObject `json:"schema"`
}

type operationParameter struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	In          string        `json:"in"`
	Schema      *schemaObject `json:"schema"`
}

type operationDefinition struct {
	OperationID string `json:"operationId" yaml:"operationId"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	RequestBody struct {
		Required bool                    `json:"required"`
		Content  map[string]*bodyContent `json:"content"`
	} `json:"requestBody" yaml:"requestBody"`
	Parameters []*operationParameter `json:"parameters"` // includes path (group) vars

	doc *schemaDocument
}

func (doc *schemaDocument) BaseURL(rootURL string) (*url.URL, error) {
	root, err := url.Parse(rootURL)
	if err != nil {
		return nil, fmt.Errorf("parse root url: %w", err)
	}

	// a little cleanup
	root.RawQuery = ""
	root.RawFragment = ""
	root.Fragment = ""
	root.User = nil

	for _, srv := range doc.Servers {
		link, err := url.Parse(srv.URL)
		if err != nil {
			return nil, fmt.Errorf("parse server url: %w", err)
		}
		if link.IsAbs() { // zero thoughts
			return link, nil
		}
		if root.IsAbs() { // append and return
			return root.ResolveReference(link), nil
		}
	}

	// no servers but root is abs
	if root.IsAbs() {
		return root, nil
	}

	return nil, errors.New("impossible to detect root API URL")
}

func (op *operationDefinition) convertRefs() {
	for _, c := range op.RequestBody.Content {
		if c.Schema != nil {
			c.Schema.convertRefs()
		}
	}
	for _, p := range op.Parameters {
		p.convertRefs()
	}
}

type httpClientFunc func(req *http.Request) (*http.Response, error)

// Input for LLM [toolRequest].
func (op *operationDefinition) toolInput() (*jsonschema.Schema, error) {
	var schema = &jsonschema.Schema{
		Type:       "object",
		Properties: orderedmap.New[string, *jsonschema.Schema](),
	}

	pathObj := &jsonschema.Schema{
		Type:       "object",
		Properties: orderedmap.New[string, *jsonschema.Schema](),
	}
	schema.Properties.Set("path", pathObj)
	queryObj := &jsonschema.Schema{
		Type:       "object",
		Properties: orderedmap.New[string, *jsonschema.Schema](),
	}
	schema.Properties.Set("query", queryObj)
	headerObj := &jsonschema.Schema{
		Type:       "object",
		Properties: orderedmap.New[string, *jsonschema.Schema](),
	}
	schema.Properties.Set("header", headerObj)

	var (
		hasPath    bool
		hasQuery   bool
		hasBody    bool
		hasHeaders bool
	)

	if op.RequestBody.Content != nil {
		js, ok := op.RequestBody.Content["application/json"]
		if !ok {
			return nil, fmt.Errorf("content for operation %q does not support application/json", op.OperationID)
		}
		schema.Properties.Set("body", js.Schema.Schema())
		if op.RequestBody.Required {
			schema.Required = append(schema.Required, "body")
		}
		hasBody = true
	}

	var dedup = make(map[paramID]bool, len(op.Parameters))

	for _, param := range op.Parameters {
		pid := paramID{In: param.In, Name: param.Name}
		if dedup[pid] {
			return nil, fmt.Errorf("duplicate parameter %+v", pid)
		}
		dedup[pid] = true

		sch := param.Schema.Schema()
		if param.Description != "" {
			sch.Description = strings.Trim(param.Description+". "+sch.Description, ". \n\t")
		}
		switch param.In {
		case "path":
			hasPath = true
			pathObj.Properties.Set(param.Name, sch)
			if param.Required {
				pathObj.Required = append(pathObj.Required, param.Name)
			}
		case "query":
			hasQuery = true
			queryObj.Properties.Set(param.Name, sch)
			if param.Required {
				queryObj.Required = append(queryObj.Required, param.Name)
			}
		case "header":
			hasHeaders = true
			headerObj.Properties.Set(param.Name, sch)
			if param.Required {
				headerObj.Required = append(headerObj.Required, param.Name)
			}
		default:
			return nil, fmt.Errorf("unknown parameter %q location %q in operation %q", param.Name, param.In, op.OperationID)
		}
	}

	if !hasBody {
		schema.Properties.Delete("body")
	}
	if !hasQuery {
		schema.Properties.Delete("query")
	}
	if !hasPath {
		schema.Properties.Delete("path")
	}
	if !hasHeaders {
		schema.Properties.Delete("header")
	}

	def, err := op.doc.Dependencies(schema)
	if err != nil {
		return nil, fmt.Errorf("collect dependencies: %w", err)
	}
	schema.Definitions = def

	return schema, nil
}

func (p *operationParameter) convertRefs() {
	if p.Schema != nil {
		p.Schema.convertRefs()
	}
}

func (doc *schemaDocument) Operation(id string) (*operationDefinition, error) {
	for _, p := range doc.Paths {
		for _, op := range p.Operations {
			if op.OperationID == id {
				return op, nil
			}
		}
	}
	return nil, fmt.Errorf("operation %q not found", id)
}

func (doc *schemaDocument) Dependencies(root *jsonschema.Schema) (jsonschema.Definitions, error) {
	var ans = make(jsonschema.Definitions)

	return ans, doc.WalkRefs(root, func(parent *jsonschema.Schema, ref string, target *jsonschema.Schema) error {
		name := strings.TrimPrefix(ref, "#/$defs/")
		ans[name] = target
		return nil
	})
}

func (doc *schemaDocument) convertRefs() {
	// replaces /components/schema to $defs
	for _, p := range doc.Paths {
		p.convertRefs()
	}

	for _, def := range doc.Components.Schemas {
		def.convertRefs()
	}
}

func (doc *schemaDocument) appendParams() {
	for _, p := range doc.Paths {
		p.appendParams()
	}
}

func (doc *schemaDocument) linkDoc() {
	for _, p := range doc.Paths {
		p.linkDoc(doc)
	}
}

func (doc *schemaDocument) WalkRefs(root *jsonschema.Schema, handler func(parent *jsonschema.Schema, ref string, target *jsonschema.Schema) error) error {
	return doc.walk(root, handler, utils.NewSet[string]())
}

func (doc *schemaDocument) walk(root *jsonschema.Schema, handler func(parent *jsonschema.Schema, ref string, target *jsonschema.Schema) error, out utils.Set[string]) error {
	var refs []string

	if root.Ref != "" {
		if out.Put(root.Ref) {
			refs = append(refs, root.Ref)
		}
	}

	if root.Items != nil && root.Items.Ref != "" {
		if out.Put(root.Items.Ref) {
			refs = append(refs, root.Items.Ref)
		}
	}

	if root.Properties != nil {
		for k := root.Properties.Oldest(); k != nil; k = k.Next() {
			if err := doc.walk(k.Value, handler, out); err != nil {
				return err
			}
		}
	}

	for _, ref := range refs {
		s, ok := doc.Resolve(ref)
		if !ok {
			return fmt.Errorf("%q is not a valid ref", ref)
		}
		if err := doc.walk(s, handler, out); err != nil {
			return err
		}
		if err := handler(root, ref, s); err != nil {
			return err
		}
	}

	return nil
}

func (doc *schemaDocument) Resolve(ref string) (*jsonschema.Schema, bool) {
	if cached, ok := doc.cache[ref]; ok {
		return cached, true
	}
	name := strings.TrimPrefix(ref, "#/$defs/")
	v, ok := doc.Components.Schemas[name]
	if !ok {
		return nil, false
	}
	if doc.cache == nil {
		doc.cache = make(map[string]*jsonschema.Schema)
	}
	sch := v.Schema()
	doc.cache[ref] = sch
	return sch, true
}

type schemaPath struct {
	Parameters []*operationParameter           `json:"parameters"`
	Operations map[string]*operationDefinition `yaml:",inline"`
}

func (p *schemaPath) convertRefs() {
	for _, op := range p.Operations {
		op.convertRefs()
	}
	for _, pr := range p.Parameters {
		pr.convertRefs()
	}
}

func (p *schemaPath) appendParams() {
	for _, op := range p.Operations {
		op.Parameters = append(op.Parameters, p.Parameters...)
	}
	p.Parameters = nil
}

func (p *schemaPath) linkDoc(doc *schemaDocument) {
	for _, op := range p.Operations {
		op.doc = doc
	}
}

type schemaObject struct {
	Type        string                   `json:"type"`
	Format      string                   `json:"format,omitempty"`
	Minimum     json.Number              `json:"minimum,omitempty"`
	Maximum     json.Number              `json:"maximum,omitempty"`
	MinLength   json.Number              `json:"minLength,omitempty"`
	MaxLength   json.Number              `json:"maxLength,omitempty"`
	Example     any                      `json:"example,omitempty"`
	Default     any                      `json:"default,omitempty"`
	Pattern     string                   `json:"pattern,omitempty"`
	MinItems    *uint64                  `json:"minItems,omitempty"`
	MaxItems    *uint64                  `json:"maxItems,omitempty"`
	Enum        []any                    `json:"enum,omitempty"`
	Ref         string                   `json:"$ref,omitempty" yaml:"$ref"`
	Description string                   `json:"description,omitempty"`
	Properties  map[string]*schemaObject `json:"properties,omitempty"`
	Required    []string                 `json:"required,omitempty"`
	Items       *schemaObject            `json:"items,omitempty"`
}

func (so *schemaObject) convertRefs() {
	if so.Ref != "" {
		so.Ref = "#/$defs/" + strings.TrimPrefix(so.Ref, "#/components/schemas/")
	}
	if so.Items != nil {
		so.Items.convertRefs()
	}
	for _, v := range so.Properties {
		v.convertRefs()
	}
}

func (so *schemaObject) Schema() *jsonschema.Schema {
	var out = &jsonschema.Schema{
		Type:        so.Type,
		Format:      so.Format,
		Minimum:     so.Minimum,
		Maximum:     so.Maximum,
		MinItems:    so.MinItems,
		MaxItems:    so.MaxItems,
		Pattern:     so.Pattern,
		Enum:        so.Enum,
		Ref:         so.Ref,
		Description: so.Description,
		Required:    so.Required,
		Default:     so.Default,
	}

	if so.Example != nil {
		out.Examples = append(out.Examples, so.Example)
	}

	if len(so.Properties) > 0 {
		out.Properties = orderedmap.New[string, *jsonschema.Schema]()
		for k, v := range so.Properties {
			out.Properties.Set(k, v.Schema())
		}
	}

	if so.Items != nil {
		out.Items = so.Items.Schema()
	}

	return out
}

type paramID struct {
	In   string
	Name string
}
