// Package manifest parses and validates the backend-generated CLI manifest
// (cli-manifest.json) that drives the dynamic command tree.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Param types accepted by the manifest schema.
const (
	TypeString   = "string"
	TypeInteger  = "integer"
	TypeBoolean  = "boolean"
	TypeDate     = "date"
	TypeDatetime = "datetime"
	TypeDecimal  = "decimal"
	TypeArray    = "array"
	TypeObject   = "object"
)

// Param locations accepted by the manifest schema.
const (
	InBody  = "body"
	InQuery = "query"
)

// Manifest is the top-level cli-manifest.json document. generated_at and
// source are volatile metadata; equality against a future backend manifest is
// defined over Endpoints only.
type Manifest struct {
	ManifestVersion int        `json:"manifest_version"`
	GeneratedAt     string     `json:"generated_at"`
	Source          string     `json:"source"`
	Endpoints       []Endpoint `json:"endpoints"`
}

// Endpoint describes one backend endpoint exposed as a dynamic command.
type Endpoint struct {
	Command     []string `json:"command"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	PathParams  []string `json:"path_params"`
	BodyRoot    string   `json:"body_root"`
	Params      []Param  `json:"params"`
	Response    string   `json:"response"`
	Auth        string   `json:"auth"`
	Controller  string   `json:"controller"`

	// Pagination is set when the endpoint's JSON is paged. It is a capability,
	// not a param: the executor derives --page/--all from it, so a paged
	// endpoint does not hand-declare a page param (and every future one gets
	// the same behaviour for free).
	Pagination *Pagination `json:"pagination,omitempty"`
}

// Pagination describes how an endpoint pages its JSON response. Param is the
// query parameter that selects the page (1-based); PerPage is how many records
// a full page carries, which is what lets the executor tell "this page is full,
// there is probably more" from "this is the last page".
type Pagination struct {
	Param   string `json:"param"`
	PerPage int    `json:"per_page"`
}

// Param describes one request parameter of an endpoint.
type Param struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Desc     string   `json:"desc"`
	Enum     []string `json:"enum"`
	In       string   `json:"in"`
	Item     string   `json:"item"`
	Spread   bool     `json:"spread"`
}

// CommandPath renders the endpoint's command as a space-separated path.
func (e *Endpoint) CommandPath() string {
	return strings.Join(e.Command, " ")
}

// validTypes is the closed set of param (and array item) types.
var validTypes = map[string]bool{
	TypeString: true, TypeInteger: true, TypeBoolean: true, TypeDate: true,
	TypeDatetime: true, TypeDecimal: true, TypeArray: true, TypeObject: true,
}

// Load parses and validates the embedded manifest.
func Load() (*Manifest, error) {
	return Parse(embedded)
}

// Parse parses and validates manifest JSON.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	for i := range m.Endpoints {
		if err := m.Endpoints[i].validate(); err != nil {
			return fmt.Errorf("endpoint %d (%s): %w", i, m.Endpoints[i].CommandPath(), err)
		}
	}
	return nil
}

func (e *Endpoint) validate() error {
	if len(e.Command) == 0 {
		return fmt.Errorf("missing command")
	}
	for _, part := range e.Command {
		if part == "" {
			return fmt.Errorf("empty command element")
		}
	}
	if e.Method == "" {
		return fmt.Errorf("missing method")
	}
	if e.Path == "" {
		return fmt.Errorf("missing path")
	}
	declared := make(map[string]bool, len(e.PathParams))
	for _, pp := range e.PathParams {
		if declared[pp] {
			return fmt.Errorf("duplicate path param %q", pp)
		}
		declared[pp] = true
		if !pathHasParam(e.Path, pp) {
			return fmt.Errorf("path param %q not present in path %q", pp, e.Path)
		}
	}
	for _, seg := range strings.Split(e.Path, "/") {
		if strings.HasPrefix(seg, ":") && !declared[seg[1:]] {
			return fmt.Errorf("path segment %q not declared in path_params", seg)
		}
	}
	seen := make(map[string]bool, len(e.Params))
	for _, p := range e.Params {
		if seen[p.Name] {
			return fmt.Errorf("duplicate param %q", p.Name)
		}
		seen[p.Name] = true
		if err := p.validate(); err != nil {
			return fmt.Errorf("param %q: %w", p.Name, err)
		}
	}
	// A spread param carries the whole request body under body_root, so it
	// cannot coexist with other body params: the executor would otherwise
	// build an order-dependent, silently-incomplete payload (the spread
	// assignment discards earlier fields while later fields mutate the spread
	// value). Enforce it here so a malformed manifest fails fast at load.
	bodyParams, spreadParams := 0, 0
	for i := range e.Params {
		if e.Params[i].In == InBody {
			bodyParams++
		}
		if e.Params[i].Spread {
			spreadParams++
		}
	}
	if spreadParams > 1 {
		return fmt.Errorf("at most one spread param allowed, found %d", spreadParams)
	}
	if spreadParams == 1 && bodyParams > 1 {
		return fmt.Errorf("a spread param must be the only body param, found %d body params", bodyParams)
	}
	if err := e.validatePagination(seen); err != nil {
		return err
	}
	return nil
}

// validatePagination mirrors the backend's schema check: the page param must be
// usable as a flag name (not reserved, not already taken by a declared param)
// and per_page must be positive — otherwise the executor could not tell a full
// page from a partial one.
func (e *Endpoint) validatePagination(declaredParams map[string]bool) error {
	if e.Pagination == nil {
		return nil
	}
	name := e.Pagination.Param
	if name == "" {
		return fmt.Errorf("pagination: missing param")
	}
	if reservedParamNames[name] || name == AllFlagName {
		return fmt.Errorf("pagination: param %q would shadow the built-in --%s flag", name, name)
	}
	if declaredParams[name] {
		return fmt.Errorf("pagination: param %q collides with a declared param", name)
	}
	// --all is only added to paginated endpoints, so a param named "all" is
	// fine elsewhere but would collide here.
	if declaredParams[AllFlagName] {
		return fmt.Errorf("pagination: declared param %q collides with the --%s flag", AllFlagName, AllFlagName)
	}
	if e.Pagination.PerPage <= 0 {
		return fmt.Errorf("pagination: per_page must be positive, got %d", e.Pagination.PerPage)
	}
	return nil
}

// reservedParamNames are flag names owned by the CLI itself; a manifest param
// with one of these names would shadow (or panic on) a built-in flag.
var reservedParamNames = map[string]bool{"format": true, "help": true, "h": true}

// AllFlagName is the flag the executor adds to paginated endpoints to walk
// every page; a manifest param may not shadow it either.
const AllFlagName = "all"

func (p *Param) validate() error {
	if p.Name == "" {
		return errors.New("missing name")
	}
	if reservedParamNames[p.Name] {
		return fmt.Errorf("reserved name (would shadow the built-in --%s flag)", p.Name)
	}
	if !validTypes[p.Type] {
		return fmt.Errorf("unknown type %q", p.Type)
	}
	if p.In != InBody && p.In != InQuery {
		return fmt.Errorf("unknown location %q", p.In)
	}
	if p.Item != "" && !validTypes[p.Item] {
		return fmt.Errorf("unknown item type %q", p.Item)
	}
	if p.Spread {
		if p.Type != TypeObject {
			return errors.New("spread requires type object")
		}
		if p.In != InBody {
			return errors.New("spread requires in body")
		}
	}
	return nil
}

// pathHasParam reports whether path contains a "/:name" segment.
func pathHasParam(path, name string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == ":"+name {
			return true
		}
	}
	return false
}
