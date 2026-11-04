// Package spec parses FireHydrant's public Swagger 2.0 spec into the
// operations that `fhcli api` exposes as commands.
package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Param is a path, query, or body parameter of an operation.
type Param struct {
	Name        string
	Description string
	Type        string // string | integer | number | boolean | array | object | file
	ItemType    string // element type when Type is array
	Required    bool
	Enum        []string
	Default     string // server-side default, "" when undocumented
}

// Scalar reports whether the param can be set from a single CLI flag.
func (p Param) Scalar() bool {
	switch p.Type {
	case "string", "integer", "number", "boolean":
		return true
	case "array":
		return p.ItemType == "string" || p.ItemType == "integer" || p.ItemType == "number"
	}
	return false
}

// Operation is one API endpoint + method.
type Operation struct {
	ID          string
	Tag         string
	Method      string // upper-case HTTP method
	Path        string // template, e.g. /v1/incidents/{incident_id}
	Summary     string
	Description string
	PathParams  []Param // in path-template order
	QueryParams []Param
	BodyFields  []Param // top-level body properties and formData fields
	HasBody     bool    // the operation accepts a request body

	// ResponseFields are the top-level fields of the documented 200/201
	// response, sorted; nil when the spec documents none. ResponseItemFields
	// are the fields of each element when the response has a data array
	// (paginated lists).
	ResponseFields     []string
	ResponseItemFields []string

	// SeeAlso lists related operation IDs and Notes adds usage guidance the
	// spec lacks; both come from the local overlay (overlay.go).
	SeeAlso []string
	Notes   []string
}

// Spec is the parsed set of operations.
type Spec struct {
	Operations []Operation // sorted by Tag, then ID
	Source     string      // EmbeddedSource or the override file path
	ModifiedAt time.Time   // override file mtime; zero for the embedded spec
}

type rawSpec struct {
	Paths       map[string]map[string]json.RawMessage `json:"paths"`
	Definitions map[string]rawSchema                  `json:"definitions"`
}

type rawOperation struct {
	OperationID string     `json:"operationId"`
	Tags        []string   `json:"tags"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Parameters  []rawParam `json:"parameters"`
	Responses   map[string]struct {
		Schema *rawSchema `json:"schema"`
	} `json:"responses"`
}

type rawParam struct {
	Name        string     `json:"name"`
	In          string     `json:"in"`
	Description string     `json:"description"`
	Required    bool       `json:"required"`
	Type        string     `json:"type"`
	Items       *rawSchema `json:"items"`
	Enum        []any      `json:"enum"`
	Default     any        `json:"default"`
	Schema      *rawSchema `json:"schema"`
}

type rawSchema struct {
	Ref         string               `json:"$ref"`
	Type        string               `json:"type"`
	Description string               `json:"description"`
	Items       *rawSchema           `json:"items"`
	Enum        []any                `json:"enum"`
	Default     any                  `json:"default"`
	Properties  map[string]rawSchema `json:"properties"`
	Required    []string             `json:"required"`
}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

var pathParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

// Parse parses a Swagger 2.0 document into operations and applies the local
// overlay of known spec corrections.
func Parse(data []byte) (*Spec, error) {
	s, err := parse(data)
	if err != nil {
		return nil, err
	}
	applyOverlay(s, overlay)
	return s, nil
}

// parse parses a Swagger 2.0 document into operations, without the overlay.
func parse(data []byte) (*Spec, error) {
	var raw rawSpec
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if len(raw.Paths) == 0 {
		return nil, errors.New("parse spec: no paths found")
	}

	var ops []Operation
	for path, item := range raw.Paths {
		for _, method := range httpMethods {
			data, ok := item[method]
			if !ok {
				continue
			}
			op, err := raw.operation(method, path, data)
			if err != nil {
				return nil, err
			}
			ops = append(ops, op)
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Tag != ops[j].Tag {
			return ops[i].Tag < ops[j].Tag
		}
		return ops[i].ID < ops[j].ID
	})

	return &Spec{Operations: ops}, nil
}

func (raw *rawSpec) operation(method, path string, data json.RawMessage) (Operation, error) {
	upper := strings.ToUpper(method)

	var ro rawOperation
	if err := json.Unmarshal(data, &ro); err != nil {
		return Operation{}, fmt.Errorf("parse %s %s: %w", upper, path, err)
	}
	if ro.OperationID == "" || len(ro.Tags) == 0 {
		return Operation{}, fmt.Errorf("parse %s %s: operationId and tag are required", upper, path)
	}

	op := Operation{
		ID:          ro.OperationID,
		Tag:         ro.Tags[0],
		Method:      upper,
		Path:        path,
		Summary:     ro.Summary,
		Description: ro.Description,
	}
	for _, p := range ro.Parameters {
		switch p.In {
		case "path":
			op.PathParams = append(op.PathParams, p.param())
		case "query":
			op.QueryParams = append(op.QueryParams, p.param())
		case "formData":
			// grape-swagger emits formData for some params, but Grape accepts
			// the same fields in a JSON body.
			op.HasBody = true
			op.BodyFields = append(op.BodyFields, p.param())
		case "body":
			op.HasBody = true
			if p.Schema != nil {
				op.BodyFields = append(op.BodyFields, raw.bodyFields(*p.Schema)...)
			}
		}
	}
	op.PathParams = orderByTemplate(path, op.PathParams)
	raw.responseFields(&op, ro)

	return op, nil
}

// responseFields records the fields of the 200 (or else 201) response.
func (raw *rawSpec) responseFields(op *Operation, ro rawOperation) {
	for _, code := range []string{"200", "201"} {
		resp, ok := ro.Responses[code]
		if !ok || resp.Schema == nil {
			continue
		}
		s := raw.resolve(*resp.Schema)
		op.ResponseFields = sortedKeys(s.Properties)
		if data, ok := s.Properties["data"]; ok {
			if d := raw.resolve(data); d.Type == "array" && d.Items != nil {
				op.ResponseItemFields = sortedKeys(raw.resolve(*d.Items).Properties)
			}
		}
		return
	}
}

// sortedKeys returns the property names, sorted; nil for none.
func sortedKeys(props map[string]rawSchema) []string {
	if len(props) == 0 {
		return nil
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p rawParam) param() Param {
	out := Param{
		Name:        p.Name,
		Description: p.Description,
		Type:        p.Type,
		Required:    p.Required,
		Enum:        enumStrings(p.Enum),
		Default:     defaultString(p.Default),
	}
	if p.Items != nil {
		out.ItemType = p.Items.Type
	}
	return out
}

// bodyFields returns the top-level properties of a body schema, sorted by name.
func (raw *rawSpec) bodyFields(schema rawSchema) []Param {
	s := raw.resolve(schema)

	required := make(map[string]bool, len(s.Required))
	for _, name := range s.Required {
		required[name] = true
	}

	propNames := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	fields := make([]Param, 0, len(propNames))
	for _, name := range propNames {
		declared := s.Properties[name]
		prop := raw.resolve(declared)

		description := declared.Description
		if description == "" {
			description = prop.Description
		}

		field := Param{
			Name:        name,
			Description: description,
			Type:        schemaType(prop),
			Required:    required[name],
			Enum:        enumStrings(prop.Enum),
			Default:     defaultString(prop.Default),
		}
		if prop.Items != nil {
			field.ItemType = schemaType(raw.resolve(*prop.Items))
		}
		fields = append(fields, field)
	}
	return fields
}

// resolve follows $ref chains into definitions. The depth cap guards against
// cyclic refs; an unknown ref resolves to a bare object.
func (raw *rawSpec) resolve(s rawSchema) rawSchema {
	for i := 0; i < 32 && s.Ref != ""; i++ {
		def, ok := raw.Definitions[strings.TrimPrefix(s.Ref, "#/definitions/")]
		if !ok {
			return rawSchema{Type: "object"}
		}
		s = def
	}
	return s
}

func schemaType(s rawSchema) string {
	if s.Type != "" {
		return s.Type
	}
	return "object"
}

func defaultString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func enumStrings(values []any) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprint(v)
	}
	return out
}

// orderByTemplate sorts path params into the order they appear in the path
// template, so positional CLI args read left to right like the URL.
func orderByTemplate(path string, params []Param) []Param {
	remaining := make(map[string]Param, len(params))
	for _, p := range params {
		remaining[p.Name] = p
	}

	ordered := make([]Param, 0, len(params))
	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		if p, ok := remaining[m[1]]; ok {
			ordered = append(ordered, p)
			delete(remaining, m[1])
		}
	}
	for _, p := range params {
		if _, left := remaining[p.Name]; left {
			ordered = append(ordered, p)
		}
	}
	return ordered
}
