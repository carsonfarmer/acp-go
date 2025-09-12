package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// Node represents a node in the abstract syntax tree
type Node interface {
	String() string
}

// File represents a complete Go source file
type File struct {
	Package      string
	Imports      []Import
	Declarations []Declaration
}

func (f *File) String() string {
	var parts []string
	parts = append(parts, "package "+f.Package)

	if len(f.Imports) > 0 {
		parts = append(parts, "")
		if len(f.Imports) == 1 {
			parts = append(parts, `import "`+f.Imports[0].Path+`"`)
		} else {
			parts = append(parts, "import (")
			for _, imp := range f.Imports {
				if imp.Name != "" {
					parts = append(parts, "\t"+imp.Name+` "`+imp.Path+`"`)
				} else {
					parts = append(parts, "\t\""+imp.Path+"\"")
				}
			}
			parts = append(parts, ")")
		}
	}

	for _, decl := range f.Declarations {
		parts = append(parts, "", decl.String())
	}

	return strings.Join(parts, "\n")
}

// Import represents an import statement
type Import struct {
	Name string // Optional import name
	Path string // Import path
}

// Declaration represents any top-level declaration
type Declaration interface {
	Node
	isDeclation()
}

// Comment represents a comment
type Comment struct {
	Text string
}

func (c *Comment) String() string {
	if c.Text == "" {
		return ""
	}
	lines := strings.Split(c.Text, "\n")
	var result []string
	for _, line := range lines {
		if line != "" {
			result = append(result, "// "+line)
		} else {
			result = append(result, "//")
		}
	}
	return strings.Join(result, "\n")
}

// TypeDecl represents a type declaration (type alias or type definition)
type TypeDecl struct {
	Comment *Comment
	Name    string
	Type    Type
}

func (td *TypeDecl) String() string {
	var parts []string
	if td.Comment != nil && td.Comment.Text != "" {
		parts = append(parts, td.Comment.String())
	}
	parts = append(parts, "type "+td.Name+" "+td.Type.String())
	return strings.Join(parts, "\n")
}

func (td *TypeDecl) isDeclation() {}

// ConstDecl represents a constant declaration
type ConstDecl struct {
	Comment *Comment
	Name    string
	Type    Type
	Value   string
}

func (cd *ConstDecl) String() string {
	var parts []string
	if cd.Comment != nil && cd.Comment.Text != "" {
		parts = append(parts, cd.Comment.String())
	}

	constLine := cd.Name
	if cd.Type != nil {
		constLine += " " + cd.Type.String()
	}
	constLine += " = " + cd.Value
	parts = append(parts, constLine)

	return strings.Join(parts, "\n")
}

func (cd *ConstDecl) isDeclation() {}

// ConstBlock represents a const block declaration
type ConstBlock struct {
	Comment *Comment
	Consts  []*ConstDecl
}

func (cb *ConstBlock) String() string {
	var parts []string
	if cb.Comment != nil && cb.Comment.Text != "" {
		parts = append(parts, cb.Comment.String())
	}

	parts = append(parts, "const (")
	for _, c := range cb.Consts {
		if c.Comment != nil && c.Comment.Text != "" {
			// Indent comment
			commentLines := strings.Split(c.Comment.String(), "\n")
			for _, line := range commentLines {
				if line != "" {
					parts = append(parts, "\t"+line)
				}
			}
		}
		constLine := "\t" + c.Name
		if c.Type != nil {
			constLine += " " + c.Type.String()
		}
		constLine += " = " + c.Value
		parts = append(parts, constLine)
	}
	parts = append(parts, ")")

	return strings.Join(parts, "\n")
}

// AddConst adds a const declaration to the const block
func (cb *ConstBlock) AddConst(name, goType, value, comment string) *ConstDecl {
	constDecl := &ConstDecl{
		Name:  name,
		Type:  parseType(goType),
		Value: value,
	}

	if comment != "" {
		constDecl.Comment = &Comment{Text: comment}
	}

	cb.Consts = append(cb.Consts, constDecl)
	return constDecl
}

func (cb *ConstBlock) isDeclation() {}

// VariableDecl represents a variable declaration
type VariableDecl struct {
	Comment *Comment
	Name    string
	Type    string
	Value   string
}

func (vd *VariableDecl) String() string {
	var parts []string
	if vd.Comment != nil && vd.Comment.Text != "" {
		parts = append(parts, vd.Comment.String())
	}

	varLine := "var " + vd.Name + " = " + vd.Value
	parts = append(parts, varLine)

	return strings.Join(parts, "\n")
}

func (vd *VariableDecl) isDeclation() {}

// StructDecl represents a struct declaration with its associated methods
type StructDecl struct {
	Comment *Comment
	Name    string
	Fields  []*Field
	Methods []*MethodDecl // Methods associated with this struct
}

func (sd *StructDecl) String() string {
	var parts []string
	if sd.Comment != nil && sd.Comment.Text != "" {
		parts = append(parts, sd.Comment.String())
	}

	// Struct definition
	parts = append(parts, "type "+sd.Name+" struct {")
	for _, field := range sd.Fields {
		fieldStr := "\t" + field.Name + " " + field.Type.String()
		if field.Tag != "" {
			fieldStr += " `" + field.Tag + "`"
		}
		if field.Comment != nil && field.Comment.Text != "" {
			fieldStr += " " + strings.ReplaceAll(field.Comment.String(), "\n", " ")
		}
		parts = append(parts, fieldStr)
	}
	parts = append(parts, "}")

	// Add associated methods immediately after the struct
	for _, method := range sd.Methods {
		parts = append(parts, "", method.String())
	}

	return strings.Join(parts, "\n")
}

// AddMethod adds a method to this struct
func (sd *StructDecl) AddMethod(method *MethodDecl) {
	sd.Methods = append(sd.Methods, method)
}

// CreateMethod creates a method from a Go code snippet and adds it to this struct
func (sd *StructDecl) CreateMethod(def string) (*MethodDecl, error) {
	// Wrap the method definition in a type declaration to make it parseable
	src := fmt.Sprintf("package p\ntype T struct{}\n%s", def)

	// Parse the source code
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse method definition: %w", err)
	}

	// Find the method declaration
	var methodDecl *ast.FuncDecl
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Recv != nil {
			methodDecl = fn
			return false
		}
		return true
	})

	if methodDecl == nil {
		return nil, fmt.Errorf("no method found in the provided definition")
	}

	// Convert AST method to our MethodDecl structure
	method, err := sd.convertASTMethodToMethodDecl(methodDecl, fset, def)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AST method: %w", err)
	}

	// Add the method to this struct
	sd.AddMethod(method)

	return method, nil
}

// convertASTMethodToMethodDecl converts an AST method declaration to our MethodDecl structure
func (sd *StructDecl) convertASTMethodToMethodDecl(fn *ast.FuncDecl, _ *token.FileSet, originalDef string) (*MethodDecl, error) {
	method := &MethodDecl{
		Name: fn.Name.Name,
	}

	// Extract comment if present
	if fn.Doc != nil {
		method.Comment = &Comment{Text: fn.Doc.Text()}
	}

	// Extract receiver
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0]
		receiverName := ""
		if len(recv.Names) > 0 {
			receiverName = recv.Names[0].Name
		}
		receiverType, err := sd.astExprToType(recv.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to convert receiver type: %w", err)
		}
		method.Receiver = &Field{
			Name: receiverName,
			Type: receiverType,
		}
	}

	// Extract parameters
	if fn.Type.Params != nil {
		for _, param := range fn.Type.Params.List {
			paramType, err := sd.astExprToType(param.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to convert parameter type: %w", err)
			}

			// Handle multiple names for the same type
			if len(param.Names) > 0 {
				for _, name := range param.Names {
					method.Params = append(method.Params, &Field{
						Name: name.Name,
						Type: paramType,
					})
				}
			} else {
				// Unnamed parameter
				method.Params = append(method.Params, &Field{
					Name: "",
					Type: paramType,
				})
			}
		}
	}

	// Extract results
	if fn.Type.Results != nil {
		for _, result := range fn.Type.Results.List {
			resultType, err := sd.astExprToType(result.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to convert result type: %w", err)
			}

			// Handle multiple names for the same type
			if len(result.Names) > 0 {
				for _, name := range result.Names {
					method.Results = append(method.Results, &Field{
						Name: name.Name,
						Type: resultType,
					})
				}
			} else {
				// Unnamed result
				method.Results = append(method.Results, &Field{
					Name: "",
					Type: resultType,
				})
			}
		}
	}

	// Extract body
	if fn.Body != nil {
		// Extract the method body from the original definition
		method.Body = sd.extractMethodBody(originalDef)
	}

	return method, nil
}

// CreateField creates a field with name, type, and optional tags and adds it to this struct
// Multiple tags are merged intelligently: A:"a",A:"b",C:"c" becomes A:"a,b" C:"c"
func (sd *StructDecl) CreateField(name, fieldType string, tags ...string) (*Field, error) {
	// Convert type string to Type interface
	typ := parseType(fieldType)

	field := &Field{
		Name: name,
		Type: typ,
	}

	// Merge tags if provided
	if len(tags) > 0 {
		mergedTag := sd.mergeTags(tags)
		if mergedTag != "" {
			field.Tag = mergedTag
		}
	}

	// Add field to struct
	sd.Fields = append(sd.Fields, field)
	return field, nil
}

// WithField creates a field with name, type, tags and returns the struct for method chaining (panics on error)
// Multiple tags are merged intelligently: A:"a",A:"b",C:"c" becomes A:"a,b" C:"c"
func (sd *StructDecl) WithField(name, fieldType string, tags ...string) *StructDecl {
	_, err := sd.CreateField(name, fieldType, tags...)
	if err != nil {
		panic(fmt.Sprintf("WithField failed: %v", err))
	}
	return sd
}

// WithMethod creates a method and returns the struct for method chaining (panics on error)
func (sd *StructDecl) WithMethod(def string) *StructDecl {
	_, err := sd.CreateMethod(def)
	if err != nil {
		panic(fmt.Sprintf("WithMethod failed: %v", err))
	}
	return sd
}

// mergeTags merges multiple tag strings intelligently
// Examples:
//
//	["json:\"name\"", "db:\"name\""] -> "json:\"name\" db:\"name\""
//	["json:\"id\"", "json:\"primary\"", "db:\"id\""] -> "json:\"id,primary\" db:\"id\""
func (sd *StructDecl) mergeTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}

	// Parse all tags into a map of tag_name -> []values
	tagMap := make(map[string][]string)

	for _, tag := range tags {
		if tag == "" {
			continue
		}

		// Parse individual tag: `json:"value" db:"value"`
		sd.parseTagString(tag, tagMap)
	}

	// Rebuild the tag string
	return sd.buildTagString(tagMap)
}

// parseTagString parses a tag string and adds values to the tagMap
func (sd *StructDecl) parseTagString(tag string, tagMap map[string][]string) {
	// Remove backticks if present
	tag = strings.Trim(tag, "`")

	// Simple parser for tag format: key:"value" key2:"value2"
	// This handles the most common cases
	parts := strings.Fields(tag)

	for _, part := range parts {
		colonIndex := strings.Index(part, ":")
		if colonIndex == -1 {
			continue
		}

		key := part[:colonIndex]
		value := part[colonIndex+1:]

		// Remove quotes from value
		value = strings.Trim(value, "\"")

		// Add to tagMap
		if _, exists := tagMap[key]; !exists {
			tagMap[key] = []string{}
		}
		tagMap[key] = append(tagMap[key], value)
	}
}

// buildTagString constructs the final tag string from tagMap
func (sd *StructDecl) buildTagString(tagMap map[string][]string) string {
	if len(tagMap) == 0 {
		return ""
	}

	var parts []string

	// Sort keys for consistent output
	keys := make([]string, 0, len(tagMap))
	for key := range tagMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		values := tagMap[key]
		if len(values) == 0 {
			continue
		}

		// Join multiple values with comma
		joinedValue := strings.Join(values, ",")
		parts = append(parts, fmt.Sprintf("%s:\"%s\"", key, joinedValue))
	}

	return strings.Join(parts, " ")
}

// extractMethodBody extracts the method body from the original method definition
func (sd *StructDecl) extractMethodBody(originalDef string) string {
	// Find the opening and closing braces
	lines := strings.Split(originalDef, "\n")
	var bodyLines []string
	inBody := false
	braceCount := 0

	for _, line := range lines {
		if !inBody {
			// Look for opening brace
			if strings.Contains(line, "{") {
				inBody = true
				braceCount = 1
				// Extract content after opening brace on the same line
				if idx := strings.Index(line, "{"); idx >= 0 && idx < len(line)-1 {
					afterBrace := strings.TrimSpace(line[idx+1:])
					if afterBrace != "" {
						bodyLines = append(bodyLines, afterBrace)
					}
				}
				continue
			}
		} else {
			// Count braces to handle nested blocks
			for _, char := range line {
				switch char {
				case '{':
					braceCount++
				case '}':
					braceCount--
				}
			}

			if braceCount == 0 {
				// Found closing brace
				if idx := strings.LastIndex(line, "}"); idx > 0 {
					beforeBrace := strings.TrimSpace(line[:idx])
					if beforeBrace != "" {
						bodyLines = append(bodyLines, beforeBrace)
					}
				}
				break
			} else {
				bodyLines = append(bodyLines, line)
			}
		}
	}

	return strings.Join(bodyLines, "\n")
}

// astExprToType converts an AST expression to our Type interface
func (sd *StructDecl) astExprToType(expr ast.Expr) (Type, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		return &BasicType{Name: e.Name}, nil
	case *ast.StarExpr:
		elem, err := sd.astExprToType(e.X)
		if err != nil {
			return nil, err
		}
		return &PointerType{Elem: elem}, nil
	case *ast.ArrayType:
		elem, err := sd.astExprToType(e.Elt)
		if err != nil {
			return nil, err
		}
		if e.Len == nil {
			// Slice type
			return &SliceType{Elem: elem}, nil
		} else {
			// Array type - convert length expression to string
			return &ArrayType{Len: astExprToString(e.Len), Elem: elem}, nil
		}
	case *ast.MapType:
		key, err := sd.astExprToType(e.Key)
		if err != nil {
			return nil, err
		}
		value, err := sd.astExprToType(e.Value)
		if err != nil {
			return nil, err
		}
		return &MapType{Key: key, Value: value}, nil
	case *ast.SelectorExpr:
		// Handle qualified identifiers like pkg.Type
		pkg := astExprToString(e.X)
		return &BasicType{Name: pkg + "." + e.Sel.Name}, nil
	default:
		// Fallback: convert to string representation
		return &BasicType{Name: astExprToString(expr)}, nil
	}
}

// astExprToString converts an AST expression to its string representation
func astExprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		return e.Value
	case *ast.SelectorExpr:
		return astExprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + astExprToString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + astExprToString(e.Elt)
		}
		return "[" + astExprToString(e.Len) + "]" + astExprToString(e.Elt)
	case *ast.MapType:
		return "map[" + astExprToString(e.Key) + "]" + astExprToString(e.Value)
	default:
		return fmt.Sprintf("%T", e) // Fallback to type name
	}
}

func (sd *StructDecl) isDeclation() {}

// MethodDecl represents a method declaration
type MethodDecl struct {
	Comment  *Comment
	Receiver *Field
	Name     string
	Params   []*Field
	Results  []*Field
	Body     string
}

func (md *MethodDecl) String() string {
	var parts []string
	if md.Comment != nil && md.Comment.Text != "" {
		parts = append(parts, md.Comment.String())
	}

	methodLine := "func "
	if md.Receiver != nil {
		methodLine += "(" + md.Receiver.Name + " " + md.Receiver.Type.String() + ") "
	}
	methodLine += md.Name + "("

	var params []string
	for _, param := range md.Params {
		paramStr := param.Name + " " + param.Type.String()
		params = append(params, paramStr)
	}
	methodLine += strings.Join(params, ", ") + ")"

	if len(md.Results) > 0 {
		if len(md.Results) == 1 && md.Results[0].Name == "" {
			methodLine += " " + md.Results[0].Type.String()
		} else {
			var results []string
			for _, result := range md.Results {
				if result.Name != "" {
					results = append(results, result.Name+" "+result.Type.String())
				} else {
					results = append(results, result.Type.String())
				}
			}
			methodLine += " (" + strings.Join(results, ", ") + ")"
		}
	}

	parts = append(parts, methodLine+" {")
	if md.Body != "" {
		bodyLines := strings.Split(md.Body, "\n")
		for _, line := range bodyLines {
			if line != "" {
				parts = append(parts, "\t"+line)
			} else {
				parts = append(parts, "")
			}
		}
	}
	parts = append(parts, "}")

	return strings.Join(parts, "\n")
}

func (md *MethodDecl) isDeclation() {}

// Field represents a field in a struct or parameter/result in a function
type Field struct {
	Comment *Comment
	Name    string
	Type    Type
	Tag     string
}

// Type represents a Go type
type Type interface {
	String() string
}

// BasicType represents basic types like string, int, bool
type BasicType struct {
	Name string
}

func (bt *BasicType) String() string {
	return bt.Name
}

// PointerType represents a pointer type
type PointerType struct {
	Elem Type
}

func (pt *PointerType) String() string {
	return "*" + pt.Elem.String()
}

// SliceType represents a slice type
type SliceType struct {
	Elem Type
}

func (st *SliceType) String() string {
	return "[]" + st.Elem.String()
}

// ArrayType represents an array type
type ArrayType struct {
	Len  string
	Elem Type
}

func (at *ArrayType) String() string {
	return "[" + at.Len + "]" + at.Elem.String()
}

// MapType represents a map type
type MapType struct {
	Key   Type
	Value Type
}

func (mt *MapType) String() string {
	return "map[" + mt.Key.String() + "]" + mt.Value.String()
}

// FuncType represents a function type
type FuncType struct {
	Params  []*Field
	Results []*Field
}

func (ft *FuncType) String() string {
	funcStr := "func("
	var params []string
	for _, param := range ft.Params {
		params = append(params, param.Type.String())
	}
	funcStr += strings.Join(params, ", ") + ")"

	if len(ft.Results) > 0 {
		if len(ft.Results) == 1 {
			funcStr += " " + ft.Results[0].Type.String()
		} else {
			var results []string
			for _, result := range ft.Results {
				results = append(results, result.Type.String())
			}
			funcStr += " (" + strings.Join(results, ", ") + ")"
		}
	}

	return funcStr
}

// ChannelType represents a channel type
type ChannelType struct {
	Dir  token.Token // ARROW, CHAN
	Elem Type
}

func (ct *ChannelType) String() string {
	switch ct.Dir {
	case token.ARROW:
		return "<-chan " + ct.Elem.String()
	default:
		return "chan " + ct.Elem.String()
	}
}

// InterfaceType represents an interface type
type InterfaceType struct {
	Methods []*Field
}

func (it *InterfaceType) String() string {
	if len(it.Methods) == 0 {
		return "interface{}"
	}

	parts := []string{"interface {"}
	for _, method := range it.Methods {
		parts = append(parts, "\t"+method.Name+method.Type.String())
	}
	parts = append(parts, "}")

	return strings.Join(parts, "\n")
}

// parseType converts a type string to a Type AST node
func parseType(typeStr string) Type {
	if typeStr == "" {
		return &BasicType{Name: "interface{}"}
	}

	// Handle pointer types
	if strings.HasPrefix(typeStr, "*") {
		return &PointerType{Elem: parseType(typeStr[1:])}
	}

	// Handle slice types
	if strings.HasPrefix(typeStr, "[]") {
		return &SliceType{Elem: parseType(typeStr[2:])}
	}

	// Handle array types
	if strings.HasPrefix(typeStr, "[") && strings.Contains(typeStr, "]") {
		closeBracket := strings.Index(typeStr, "]")
		length := typeStr[1:closeBracket]
		elem := typeStr[closeBracket+1:]
		return &ArrayType{Len: length, Elem: parseType(elem)}
	}

	// Handle map types
	if strings.HasPrefix(typeStr, "map[") {
		// Simple parsing - find the key and value types
		keyStart := 4 // after "map["
		bracketCount := 0
		keyEnd := -1

		for i := keyStart; i < len(typeStr); i++ {
			switch typeStr[i] {
			case '[':
				bracketCount++
			case ']':
				if bracketCount == 0 {
					keyEnd = i
					break
				}
				bracketCount--
			}
			if keyEnd != -1 {
				break
			}
		}

		if keyEnd != -1 {
			keyType := typeStr[keyStart:keyEnd]
			valueType := typeStr[keyEnd+1:]
			return &MapType{
				Key:   parseType(keyType),
				Value: parseType(valueType),
			}
		}
	}

	// Handle channel types
	if strings.HasPrefix(typeStr, "chan ") {
		return &ChannelType{Elem: parseType(typeStr[5:])}
	}

	if strings.HasPrefix(typeStr, "<-chan ") {
		return &ChannelType{Elem: parseType(typeStr[7:])}
	}

	// Handle interface types
	if typeStr == "interface{}" || typeStr == "any" {
		return &InterfaceType{Methods: []*Field{}}
	}

	// Default to basic type
	return &BasicType{Name: typeStr}
}
