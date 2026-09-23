// Package tsdef reads the TypeScript subset used by ACP's generated schemas.
// It does not execute TypeScript or Zod code.
package tsdef

// Schema is independent of the parser's C-owned syntax tree.
type Schema struct {
	Validators map[string]*Zod
	Types      []Definition
	Constants  []Constant
}

type Definition struct {
	Name    string
	Comment string
	Type    *Type
}

// Type retains unions and intersections rather than flattening away wire semantics.
type Type struct {
	Number  string // Go integer representation inferred from Zod numeric builders.
	Kind    string
	Name    string
	Literal string
	Members []*Type
	Fields  []Field
	Element *Type
}

type Field struct {
	Name     string
	Comment  string
	Optional bool
	Type     *Type
}

type Constant struct {
	Name    string
	Value   string
	Members []Constant
}
