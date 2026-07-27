// Package toolruntime provides the explicit executable tool registry and the
// three read-only Prowl context tools used by the bounded agent tracer.
//
// Every tool declares a stable name, a canonical JSON schema, a required
// permission set, input/output bounds, an availability predicate, and a
// handler. Registration fails closed on duplicate names or invalid/unstable
// schemas. Execution is deny-by-default: a session pins the exact set of tools
// it may call, permission is evaluated before any handler side effect, and a
// tool added to the registry after a session pinned its toolset is never
// resolvable mid-session.
package toolruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

// PermissionClass is a closed capability category a tool may require.
type PermissionClass string

const (
	PermissionReadOnly PermissionClass = "read_only"
	PermissionWrite    PermissionClass = "write"
	PermissionNetwork  PermissionClass = "network"
	PermissionProcess  PermissionClass = "process"
)

func (c PermissionClass) valid() bool {
	switch c {
	case PermissionReadOnly, PermissionWrite, PermissionNetwork, PermissionProcess:
		return true
	default:
		return false
	}
}

// Sentinel errors. Every denial is fail-closed.
var (
	ErrInvalidTool      = errors.New("toolruntime: invalid tool registration")
	ErrDuplicateTool    = errors.New("toolruntime: duplicate tool name")
	ErrInvalidSchema    = errors.New("toolruntime: invalid or unstable tool schema")
	ErrToolNotFound     = errors.New("toolruntime: tool not found")
	ErrToolUnavailable  = errors.New("toolruntime: tool unavailable")
	ErrPermissionDenied = errors.New("toolruntime: permission denied")
	ErrSchemaMismatch   = errors.New("toolruntime: pinned tool schema mismatch")
	ErrInputTooLarge    = errors.New("toolruntime: tool input exceeds bound")
)

// Bounds caps the size of a tool's input and output payloads.
type Bounds struct {
	MaxInputBytes  int
	MaxOutputBytes int
}

func (b Bounds) valid() bool { return b.MaxInputBytes > 0 && b.MaxOutputBytes > 0 }

// Result is a tool handler's bounded output. IsError marks a domain-level tool
// failure that should be surfaced to the model as a tool result rather than
// aborting execution.
type Result struct {
	Content string
	IsError bool
}

// Handler executes a tool with validated, bounded input.
type Handler func(ctx context.Context, input json.RawMessage) (Result, error)

// Availability reports whether a tool may currently be offered and executed. A
// nil predicate means always available.
type Availability func(ctx context.Context) bool

// Tool is one explicit registration.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Permissions []PermissionClass
	Bounds      Bounds
	Available   Availability
	Handler     Handler
}

// Definition is the immutable public description of a registered tool.
type Definition struct {
	Name        string
	Description string
	Schema      json.RawMessage
	SchemaHash  string
	Permissions []PermissionClass
	Bounds      Bounds
}

// Call is a request to execute a registered tool.
type Call struct {
	Name  string
	Input json.RawMessage
	// Requested carries any additional capability classes a caller asks for
	// beyond the tool's declared set. A widened request that exceeds the grant
	// is denied.
	Requested []PermissionClass
	// PinnedSchemaHash, when set, must match the registered tool's canonical
	// schema hash; otherwise execution is refused.
	PinnedSchemaHash string
}

type registration struct {
	def     Definition
	handler Handler
	avail   Availability
}

// Registry is an explicit, closed set of executable tools.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]registration
	order  []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]registration{}}
}

// Register adds a tool. It fails closed on an empty name, missing handler,
// nonpositive bounds, an empty or invalid permission set, a duplicate name, or
// an invalid/unstable schema.
func (r *Registry) Register(tool Tool) error {
	if tool.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidTool)
	}
	if tool.Handler == nil {
		return fmt.Errorf("%w: handler is required", ErrInvalidTool)
	}
	if !tool.Bounds.valid() {
		return fmt.Errorf("%w: bounds must be positive", ErrInvalidTool)
	}
	perms, err := normalizePermissions(tool.Permissions)
	if err != nil {
		return err
	}
	schema, hash, err := canonicalSchema(tool.Schema)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[tool.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, tool.Name)
	}
	r.byName[tool.Name] = registration{
		def: Definition{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      schema,
			SchemaHash:  hash,
			Permissions: perms,
			Bounds:      tool.Bounds,
		},
		handler: tool.Handler,
		avail:   tool.Available,
	}
	r.order = append(r.order, tool.Name)
	sort.Strings(r.order)
	return nil
}

// Names returns the sorted registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.order)
}

// Definition returns the public description of a registered tool.
func (r *Registry) Definition(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.byName[name]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(reg.def), true
}

// Definitions returns every tool description in sorted name order.
func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, cloneDefinition(r.byName[name].def))
	}
	return out
}

// Pinned is an immutable snapshot of the tools a session may execute together
// with their canonical schema hashes. Tools added to the registry after a
// session pins its toolset are never resolvable through that pin.
type Pinned struct {
	hashes map[string]string
}

// Names returns the sorted pinned tool names.
func (p Pinned) Names() []string {
	names := make([]string, 0, len(p.hashes))
	for name := range p.hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Pin captures the current schema hashes for the named tools. An unknown name
// fails closed.
func (r *Registry) Pin(names ...string) (Pinned, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		reg, ok := r.byName[name]
		if !ok {
			return Pinned{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
		}
		hashes[name] = reg.def.SchemaHash
	}
	return Pinned{hashes: hashes}, nil
}

// PinAll captures every currently registered tool.
func (r *Registry) PinAll() Pinned {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hashes := make(map[string]string, len(r.order))
	for _, name := range r.order {
		hashes[name] = r.byName[name].def.SchemaHash
	}
	return Pinned{hashes: hashes}
}

// Execute resolves, permits, and runs a tool call. Resolution and permission
// evaluation occur before any handler side effect: the call name must be in the
// pinned set, the registration must still match the pinned schema hash, the
// availability predicate must pass, and the required permission set (the tool's
// declared classes together with any widened request) must be a subset of the
// grant. Input is bounded before the handler runs; output is bounded after.
func (r *Registry) Execute(ctx context.Context, pinned Pinned, grant Grant, call Call) (Result, error) {
	pinnedHash, ok := pinned.hashes[call.Name]
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
	}
	r.mu.RLock()
	reg, exists := r.byName[call.Name]
	r.mu.RUnlock()
	if !exists {
		return Result{}, fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
	}
	if reg.def.SchemaHash != pinnedHash {
		return Result{}, fmt.Errorf("%w: %s", ErrSchemaMismatch, call.Name)
	}
	if call.PinnedSchemaHash != "" && call.PinnedSchemaHash != reg.def.SchemaHash {
		return Result{}, fmt.Errorf("%w: %s", ErrSchemaMismatch, call.Name)
	}

	required := append(slices.Clone(reg.def.Permissions), call.Requested...)
	if err := Evaluate(grant, required); err != nil {
		return Result{}, fmt.Errorf("%w: %s", err, call.Name)
	}

	if reg.avail != nil && !reg.avail(ctx) {
		return Result{}, fmt.Errorf("%w: %s", ErrToolUnavailable, call.Name)
	}

	if len(call.Input) > reg.def.Bounds.MaxInputBytes {
		return Result{}, fmt.Errorf("%w: %s (%d > %d)", ErrInputTooLarge, call.Name, len(call.Input), reg.def.Bounds.MaxInputBytes)
	}

	result, err := reg.handler(ctx, call.Input)
	if err != nil {
		return Result{}, err
	}
	if max := reg.def.Bounds.MaxOutputBytes; len(result.Content) > max {
		result.Content = boundString(result.Content, max)
	}
	return result, nil
}

func boundString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	// Trim to a valid UTF-8 boundary at or below the byte limit.
	cut := max
	for cut > 0 && !isUTF8Start(value[cut]) {
		cut--
	}
	return value[:cut]
}

func isUTF8Start(b byte) bool { return b&0xC0 != 0x80 }

func normalizePermissions(perms []PermissionClass) ([]PermissionClass, error) {
	if len(perms) == 0 {
		return nil, fmt.Errorf("%w: at least one permission class is required", ErrInvalidTool)
	}
	seen := map[PermissionClass]struct{}{}
	out := make([]PermissionClass, 0, len(perms))
	for _, class := range perms {
		if !class.valid() {
			return nil, fmt.Errorf("%w: unknown permission class %q", ErrInvalidTool, class)
		}
		if _, dup := seen[class]; dup {
			continue
		}
		seen[class] = struct{}{}
		out = append(out, class)
	}
	slices.Sort(out)
	return out, nil
}

// canonicalSchema validates a tool schema and returns a stable canonical byte
// form plus its SHA-256 hash. A schema must be a JSON object that declares a
// "type"; anything else fails closed as invalid/unstable.
func canonicalSchema(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, "", fmt.Errorf("%w: empty", ErrInvalidSchema)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var probe any
	if err := decoder.Decode(&probe); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidSchema, err)
	}
	if decoder.More() {
		return nil, "", fmt.Errorf("%w: trailing data", ErrInvalidSchema)
	}
	object, ok := probe.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("%w: schema must be a JSON object", ErrInvalidSchema)
	}
	if _, ok := object["type"]; !ok {
		return nil, "", fmt.Errorf("%w: schema must declare a type", ErrInvalidSchema)
	}
	canonical, err := json.Marshal(probe) // Go marshals map keys in sorted order.
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidSchema, err)
	}
	sum := sha256.Sum256(canonical)
	return json.RawMessage(canonical), hex.EncodeToString(sum[:]), nil
}

func cloneDefinition(def Definition) Definition {
	def.Schema = slices.Clone(def.Schema)
	def.Permissions = slices.Clone(def.Permissions)
	return def
}

// Grant is the set of capability classes a session is allowed to exercise.
type Grant struct {
	allowed map[PermissionClass]struct{}
}

// NewGrant builds a grant from a set of valid permission classes. Unknown
// classes are ignored so a grant can never widen beyond the closed set.
func NewGrant(classes ...PermissionClass) Grant {
	allowed := make(map[PermissionClass]struct{}, len(classes))
	for _, class := range classes {
		if class.valid() {
			allowed[class] = struct{}{}
		}
	}
	return Grant{allowed: allowed}
}

// ReadOnlyGrant is the grant a normal tracer session receives.
func ReadOnlyGrant() Grant { return NewGrant(PermissionReadOnly) }

// Permits reports whether a single class is granted.
func (g Grant) Permits(class PermissionClass) bool {
	_, ok := g.allowed[class]
	return ok
}

// Evaluate is deny-by-default: an empty required set is denied, and every
// required class must be valid and present in the grant.
func Evaluate(grant Grant, required []PermissionClass) error {
	if len(required) == 0 {
		return ErrPermissionDenied
	}
	for _, class := range required {
		if !class.valid() || !grant.Permits(class) {
			return ErrPermissionDenied
		}
	}
	return nil
}
