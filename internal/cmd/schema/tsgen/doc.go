// Doc comments. The SDK's comments are JSDoc written for its TypeScript and
// Rust SDKs; these rules turn them into Go doc comments. Each rule applies to
// every declaration, so a new schema type is covered without a special case.

package tsgen

import (
	"fmt"
	"regexp"
	"strings"
)

// doc renders the doc comment of a declaration from the SDK's comment and
// the generator's own paragraphs about the Go shape. The comment starts with
// the declared name where the SDK's text allows it, and otherwise with the
// first generated paragraph when that names the declaration. Stability notes
// move to one closing Experimental paragraph, and markdown links become Go doc
// links with their definitions at the end.
func doc(name, sdk string, generated ...string) string {
	sdk, experimental := stability(sdk)
	var paragraphs []string
	led := false
	if sdk != "" {
		var first string
		first, led = leadWithName(name, sdk)
		paragraphs = append(paragraphs, first)
	}
	var extra []string
	for _, p := range generated {
		if p != "" {
			extra = append(extra, p)
		}
	}
	if !led && len(extra) > 0 && strings.HasPrefix(extra[0], name+" ") {
		paragraphs = append([]string{extra[0]}, paragraphs...)
		extra = extra[1:]
	}
	paragraphs = append(paragraphs, extra...)
	if experimental {
		paragraphs = append(paragraphs, ExperimentalNote)
	}
	text, definitions := linkDefinitions(strings.Join(paragraphs, "\n\n"))
	if definitions != "" {
		text += "\n\n" + definitions
	}
	return comment(text)
}

// fieldDoc renders a struct field's comment. Fields are shown as code in
// godoc, so only the stability rule applies.
func fieldDoc(sdk string) string {
	sdk, experimental := stability(sdk)
	if experimental {
		sdk = strings.TrimSpace(sdk + "\n\n" + ExperimentalNote)
	}
	return sdk
}

// ExperimentalNote closes the comment of every declaration the SDK marks
// unstable, in generated types and façades alike.
const ExperimentalNote = "Experimental: not part of the spec yet; it may change or be removed."

// stability removes the SDK's instability markers: a **UNSTABLE** paragraph,
// the stock paragraph explaining it and a JSDoc @experimental tag. It reports
// whether any was present, for one closing note in Go's paragraph style.
func stability(sdk string) (string, bool) {
	found := false
	var kept []string
	for _, p := range paragraphs(sdk) {
		switch {
		case p == "**UNSTABLE**", p == "@experimental",
			strings.Contains(p, "not part of the spec yet") && strings.Contains(p, "may be removed or changed"):
			found = true
		default:
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n"), found
}

func paragraphs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// leadingWords rewrites the opening word of an SDK comment so the sentence
// starts with the declared name: "Request to start…" becomes "X is a request
// to start…". Articles cover most comments; the nouns cover the SDK's
// article-less openings and take an article only when a preposition or
// participle follows ("Request to…", not "Request parameters for…").
var leadingWords = map[string]struct {
	lower string
	noun  bool
}{
	"A": {lower: "a"}, "An": {lower: "an"}, "The": {lower: "the"}, "Unique": {lower: "a unique"},
	"Marker": {"a marker", true}, "Notification": {"a notification", true},
	"Request": {"a request", true}, "Response": {"a response", true},
}

var nounFollowers = map[string]bool{
	"about": true, "for": true, "from": true, "of": true, "returned": true,
	"sent": true, "that": true, "to": true, "used": true, "when": true, "with": true,
}

// finiteVerbs mark an opening that is already a sentence of its own ("The
// agent is ready…"), which cannot take "X is" in front.
var finiteVerbs = regexp.MustCompile(`\b(is|are|was|were|has|have|had|can|may|must|will|should|does|do)\b`)

// leadWithName rewrites the first paragraph to start with name where its
// opening allows, reporting whether it did. Other openings are kept.
func leadWithName(name, sdk string) (string, bool) {
	word, rest, ok := strings.Cut(sdk, " ")
	opening, known := leadingWords[word]
	if !ok || !known {
		return sdk, false
	}
	if next, _, _ := strings.Cut(rest, " "); opening.noun && !nounFollowers[next] {
		return sdk, false
	}
	sentence, _, _ := strings.Cut(sdk, ". ")
	sentence, _, _ = strings.Cut(sentence, "\n\n")
	if finiteVerbs.MatchString(sentence) {
		return sdk, false
	}
	return name + " is " + opening.lower + " " + rest, true
}

// metaDoc keeps what a _meta member's comment says beyond the extensibility
// sentences the SDK repeats on every one, which [Meta] documents once. It
// returns "" when nothing else is said.
func metaDoc(sdk string) string {
	var kept []string
	for _, p := range paragraphs(sdk) {
		if strings.Contains(p, "[Extensibility](") && strings.HasPrefix(p, "See protocol docs") {
			continue
		}
		var sentences []string
		for _, s := range strings.SplitAfter(strings.Join(strings.Fields(p), " "), ". ") {
			if s = strings.TrimSpace(s); s != "" && !strings.Contains(s, "_meta property") && !strings.Contains(s, "MUST NOT make assumptions") {
				sentences = append(sentences, s)
			}
		}
		if len(sentences) > 0 {
			kept = append(kept, strings.Join(sentences, " "))
		}
	}
	return fieldDoc(strings.Join(kept, "\n\n"))
}

var markdownLink = regexp.MustCompile(`\[([^\]\[]+)\]\((https?://[^)\s]+)\)`)

// linkDefinitions turns markdown links into Go doc links, returning the
// text and the link definitions godoc expects at the end of the comment.
func linkDefinitions(text string) (string, string) {
	var definitions []string
	seen := map[string]bool{}
	text = markdownLink.ReplaceAllStringFunc(text, func(m string) string {
		parts := markdownLink.FindStringSubmatch(m)
		label, url := parts[1], parts[2]
		if !seen[label] {
			seen[label] = true
			definitions = append(definitions, fmt.Sprintf("[%s]: %s", label, url))
		}
		return "[" + label + "]"
	})
	return text, strings.Join(definitions, "\n")
}

// rustLink matches the SDK's Rust intra-doc links: [`Type`] and [`Type::member`].
var rustLink = regexp.MustCompile("\\[`([A-Za-z_][A-Za-z0-9_]*)(?:::([A-Za-z_][A-Za-z0-9_]*))?`\\]")

// polishComments applies the rules that need every declaration to be known,
// to the comment lines of an output file: Rust intra-doc links become Go doc
// links where they resolve and plain names where they do not, escaped
// brackets are unescaped, and markdown list items are indented so godoc
// renders them as lists.
func (g *generator) polishComments(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	inList := false
	for i, line := range lines {
		indent, text, ok := strings.Cut(line, "//")
		if !ok || strings.TrimSpace(indent) != "" {
			inList = false
			continue
		}
		text = rustLink.ReplaceAllStringFunc(text, g.goDocLink)
		text = unescapeBrackets.Replace(text)
		switch {
		case strings.HasPrefix(text, " - ") || strings.HasPrefix(text, " * "):
			text, inList = "   -"+text[2:], true
		case inList && strings.TrimSpace(text) != "" && !strings.HasPrefix(text, "\t"):
			text = "    " + text // continuation of the list item
		default:
			inList = false
		}
		lines[i] = indent + "//" + text
	}
	return []byte(strings.Join(lines, "\n"))
}

var unescapeBrackets = strings.NewReplacer(`\[`, "[", `\]`, "]")

// goDocLink resolves one Rust intra-doc link against the generated package.
func (g *generator) goDocLink(m string) string {
	parts := rustLink.FindStringSubmatch(m)
	typ, member := g.declaredAs(Name(parts[1])), parts[2]
	if member == "" {
		if typ != "" {
			return "[" + typ + "]"
		}
		return Name(parts[1])
	}
	if variant := g.declaredAs(Name(parts[1]) + Name(member)); variant != "" {
		return "[" + variant + "]" // an enum variant: ContentBlock::Text
	}
	if typ != "" && g.hasField(parts[1], member) {
		return "[" + typ + "." + Name(member) + "]"
	}
	return Name(parts[1]) + "." + Name(member)
}

// declaredAs returns the Go type declared under name, following a type a
// tagged union took over to its variant, or "" when there is none.
func (g *generator) declaredAs(name string) string {
	if a := g.absorbed[name]; a != nil {
		return a.variant
	}
	if !g.names[name] {
		return ""
	}
	return name
}

// hasField reports whether the schema type named ts has a member whose Go
// name matches member's.
func (g *generator) hasField(ts, member string) bool {
	d, ok := g.defs[ts]
	if !ok {
		return false
	}
	t, err := g.expand(d, map[string]bool{})
	if err != nil {
		return false
	}
	for _, f := range t.Fields {
		if Name(f.Name) == Name(member) {
			return true
		}
	}
	return false
}
