// Package knowledge defines Prowl's domain-neutral durable knowledge model.
// Version-specific codecs live in subpackages so draft interchange formats can
// evolve without becoming the internal storage schema.
package knowledge

import "gopkg.in/yaml.v3"

// Document is a canonical Markdown knowledge document. Frontmatter retains the
// complete YAML syntax tree so codecs can preserve unknown fields and types.
type Document struct {
	Path        string
	Type        string
	Title       string
	Description string
	Resource    string
	Tags        []string
	Timestamp   string
	Body        []byte
	Prowl       Metadata
	Frontmatter *yaml.Node
}

// Metadata contains Prowl's recognized OKF extension fields. Unknown fields
// remain in Document.Frontmatter and are emitted unchanged.
type Metadata struct {
	ID         string
	Status     string
	Confidence string
	ValidFrom  string
	ValidTo    string
	Author     string
	Source     string
	Related    []string
	Anchors    []Anchor
}

// Anchor ties a knowledge claim to a deterministic source region.
type Anchor struct {
	Path        string
	LineStart   int
	LineEnd     int
	ContentHash string
	Symbol      string
}
