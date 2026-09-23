package zod

import (
	"bytes"
	"encoding/json/jsontext"
	"strconv"
	"strings"
)

// node is a parsed JSON value. The evaluator reads the input as a tree of
// nodes once, applies rules to the tree and encodes the result once, where
// it used to decode and re-encode every object and array it passed through.
//
// Nodes are never changed once built: a rule that changes a value builds a
// new node and shares the unchanged ones, so a node parsed from a rule's
// default can serve every evaluation at once.
type node struct {
	kind byte // as jsontext.Kind reports it: '{', '[', '"', '0', 't', 'f' or 'n'
	// raw is the value's JSON text: a slice of the input for a parsed node,
	// nil for an object or array a rule built, which encodes from members
	// or elements.
	raw      []byte
	members  []member // an object's properties, in input order
	elements []*node  // an array's elements
	// index finds members by name in an object with many of them.
	index map[string]int
}

// member is an object property.
type member struct {
	name  string
	value *node
}

// indexFrom is how many members an object has before lookups use an index.
const indexFrom = 16

// kindOf is n's kind, or 0 for an absent value.
func kindOf(n *node) byte {
	if n == nil {
		return 0
	}
	return n.kind
}

// get returns the value of the property name, or nil.
func (n *node) get(name string) *node {
	if n.index != nil {
		if i, ok := n.index[name]; ok {
			return n.members[i].value
		}
		return nil
	}
	for _, m := range n.members {
		if m.name == name {
			return m.value
		}
	}
	return nil
}

// text decodes a string node.
func (n *node) text() string { return unquote(n.raw) }

// unquote decodes a valid JSON string.
func unquote(raw []byte) string {
	body := raw[1 : len(raw)-1]
	if bytes.IndexByte(body, '\\') < 0 {
		return string(body)
	}
	text, _ := jsontext.AppendUnquote(nil, raw)
	return string(text)
}

// number parses a number node, reporting false for one outside float64's
// range as decoding it into a float64 would.
func (n *node) number() (float64, bool) {
	f, err := strconv.ParseFloat(string(n.raw), 64)
	return f, err == nil
}

// newObject builds an object from members.
func newObject(members []member) *node {
	n := &node{kind: '{', members: members}
	n.indexMembers()
	return n
}

// indexMembers indexes an object with many members, before it is shared.
func (n *node) indexMembers() {
	if len(n.members) > indexFrom {
		n.index = make(map[string]int, len(n.members))
		for i, m := range n.members {
			n.index[m.name] = i
		}
	}
}

// parser reads a JSON value that jsontext.Value.IsValid accepted, so it
// checks nothing: the input has no syntax errors, invalid UTF-8 or duplicate
// names. It allocates nodes in blocks.
type parser struct {
	data  []byte
	text  string // data as a string, for property names without escapes
	block []node
	grown int // the size of the last block, doubled for the next
}

// parseTree parses valid JSON into a tree of nodes.
func parseTree(data []byte) *node {
	p := parser{data: data}
	n, _ := p.value(p.space(0))
	return n
}

func (p *parser) node(kind byte) *node {
	if len(p.block) == 0 {
		p.grown = min(max(p.grown*2, 4), 64)
		p.block = make([]node, p.grown)
	}
	n := &p.block[0]
	p.block = p.block[1:]
	n.kind = kind
	return n
}

func (p *parser) space(i int) int {
	for i < len(p.data) {
		switch p.data[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// value parses the value at i and returns it with the index after it.
func (p *parser) value(i int) (*node, int) {
	start, c := i, p.data[i]
	switch c {
	case '{':
		n := p.node('{')
		i = p.space(i + 1)
		for p.data[i] != '}' {
			end := p.stringEnd(i)
			name := p.name(i, end)
			var value *node
			value, i = p.value(p.space(p.space(end) + 1)) // past the colon
			n.members = append(n.members, member{name, value})
			if i = p.space(i); p.data[i] == ',' {
				i = p.space(i + 1)
			}
		}
		i++
		n.indexMembers()
		n.raw = p.data[start:i]
		return n, i
	case '[':
		n := p.node('[')
		i = p.space(i + 1)
		for p.data[i] != ']' {
			var element *node
			element, i = p.value(i)
			n.elements = append(n.elements, element)
			if i = p.space(i); p.data[i] == ',' {
				i = p.space(i + 1)
			}
		}
		i++
		n.raw = p.data[start:i]
		return n, i
	case '"':
		i = p.stringEnd(i)
	case 't', 'n':
		i += 4
	case 'f':
		i += 5
	default:
		c = '0'
		for i < len(p.data) && strings.IndexByte("+-0123456789.eE", p.data[i]) >= 0 {
			i++
		}
	}
	n := p.node(c)
	n.raw = p.data[start:i]
	return n, i
}

// name decodes the property name data[start:end]. One copy of the input as a
// string serves every name without escapes, instead of a copy per name.
func (p *parser) name(start, end int) string {
	if bytes.IndexByte(p.data[start+1:end-1], '\\') >= 0 {
		return unquote(p.data[start:end])
	}
	if p.text == "" {
		p.text = string(p.data)
	}
	return p.text[start+1 : end-1]
}

// stringEnd returns the index after the string that starts at i.
func (p *parser) stringEnd(i int) int {
	for i++; p.data[i] != '"'; i++ {
		if p.data[i] == '\\' {
			i++
		}
	}
	return i + 1
}

// encode appends n's JSON to dst: a parsed value's own text, and a built
// object or array from its members or elements.
func encode(dst []byte, n *node) []byte {
	if n.raw != nil {
		return append(dst, n.raw...)
	}
	switch n.kind {
	case '{':
		dst = append(dst, '{')
		for i, m := range n.members {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst, _ = jsontext.AppendQuote(dst, m.name)
			dst = append(dst, ':')
			dst = encode(dst, m.value)
		}
		return append(dst, '}')
	case '[':
		dst = append(dst, '[')
		for i, e := range n.elements {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = encode(dst, e)
		}
		return append(dst, ']')
	}
	return dst
}
