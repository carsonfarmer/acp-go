// Package tsdef reads the TypeScript subset used by ACP's generated schemas.
// It does not execute TypeScript or Zod code.
package tsdef

import "strconv"

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

// Kind is the shape of a [Type].
type Kind uint8

const (
	KindInvalid Kind = iota
	KindRef          // Name is another definition

	// TypeScript's primitive types.
	KindString
	KindNumber
	KindBoolean
	KindUnknown
	KindAny
	KindNever
	KindNull

	KindLiteral      // Literal is the value as written, strings quoted
	KindUnion        // Members, nested unions flattened
	KindIntersection // Members, nested intersections flattened
	KindArray        // Element
	KindObject       // Fields, and Element for a string index signature
)

var kindNames = [...]string{"invalid", "ref", "string", "number", "boolean", "unknown", "any", "never", "null", "literal", "union", "intersection", "array", "object"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Type retains unions and intersections rather than flattening away wire semantics.
type Type struct {
	Number  string // Go integer representation inferred from Zod numeric builders.
	Kind    Kind
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
