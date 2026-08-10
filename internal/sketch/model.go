package sketch

// Sketch is the structured visual model of a UI defined as a scene/element tree
// (QML, React). Root holds the primary tree; Variants holds any additional
// top-level trees (a React component's conditional render branches).
type Sketch struct {
	File     string  `json:"file"`
	Kind     string  `json:"kind"`
	Desc     string  `json:"description,omitempty"`
	Root     *Node   `json:"root"`
	Variants []*Node `json:"variants,omitempty"`
}

// Node is one element in a UI scene tree.
type Node struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id,omitempty"`
	Slot     string   `json:"slot,omitempty"` // property this element fills on its parent (e.g. delegate)
	Props    []Prop   `json:"props,omitempty"`
	Decls    []Decl   `json:"decls,omitempty"` // properties this element declares (its API / a singleton's tokens)
	Behavior []string `json:"behavior,omitempty"`
	Children []*Node  `json:"children,omitempty"`
	Line     int      `json:"line"`
}

// Prop is a declared visual property and its source value. Resolved holds the
// literal a token reference points to (e.g. Tokens.ink -> #cdd6f4), when known.
type Prop struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Group    string `json:"group"` // geom, layout, paint, text, other
	Resolved string `json:"resolved,omitempty"`
}

// Decl is a property the component declares: its own API surface, or, for a
// theme/token singleton, the palette and metrics it exposes.
type Decl struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}
