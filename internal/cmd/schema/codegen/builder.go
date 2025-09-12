package codegen

import (
	"go/format"
	"sort"
)

// Builder provides an AST-based approach for Go code generation
// This approach provides better type safety and flexibility
type Builder struct {
	file       *File
	importsMap map[string]bool
}

// NewBuilder creates a new AST-based code generation builder
func NewBuilder(packageName string) *Builder {
	return &Builder{
		file: &File{
			Package:      packageName,
			Imports:      []Import{},
			Declarations: []Declaration{},
		},
		importsMap: make(map[string]bool),
	}
}

// AddImport adds an import to the file
func (b *Builder) AddImport(path string) {
	b.addImport("", path)
}

// AddNamedImport adds a named import to the file
func (b *Builder) AddNamedImport(name, path string) {
	b.addImport(name, path)
}

// addImport is the internal method to add imports
func (b *Builder) addImport(name, path string) {
	key := name + ":" + path
	if b.importsMap[key] {
		return
	}

	b.file.Imports = append(b.file.Imports, Import{
		Name: name,
		Path: path,
	})
	b.importsMap[key] = true
}

// AddDeclaration adds a declaration directly to the file
func (b *Builder) AddDeclaration(decl Declaration) {
	b.file.Declarations = append(b.file.Declarations, decl)
}

// CreateStruct creates a new struct and adds it to the builder
func (b *Builder) CreateStruct(name string) *StructDecl {
	structDecl := &StructDecl{
		Name:    name,
		Fields:  make([]*Field, 0),
		Methods: make([]*MethodDecl, 0),
	}

	b.AddDeclaration(structDecl)
	return structDecl
}

// CreateType creates a new type declaration and adds it to the builder
func (b *Builder) CreateType(name, goType string) *TypeDecl {
	typeDecl := &TypeDecl{
		Name: name,
		Type: parseType(goType),
	}

	b.AddDeclaration(typeDecl)
	return typeDecl
}

// CreateConstBlock creates a new const block and adds it to the builder
func (b *Builder) CreateConstBlock() *ConstBlock {
	constBlock := &ConstBlock{
		Consts: make([]*ConstDecl, 0),
	}

	b.AddDeclaration(constBlock)
	return constBlock
}

// Build generates the final Go code
func (b *Builder) Build() ([]byte, error) {
	// Sort declarations by type for consistent output
	b.sortDeclarations()

	// Generate the code string
	codeStr := b.file.String()

	// Format the generated code
	formatted, err := format.Source([]byte(codeStr))
	if err != nil {
		// Return unformatted code with formatting error as comment
		unformatted := "// Formatting error: " + err.Error() + "\n\n" + codeStr
		return []byte(unformatted), nil
	}

	return formatted, nil
}

// GetStructDeclaration finds a struct declaration by name
func (b *Builder) GetStructDeclaration(name string) *StructDecl {
	for _, decl := range b.file.Declarations {
		if structDecl, ok := decl.(*StructDecl); ok && structDecl.Name == name {
			return structDecl
		}
	}
	return nil
}

// GetTypeDeclaration finds a type declaration by name
func (b *Builder) GetTypeDeclaration(name string) *TypeDecl {
	for _, decl := range b.file.Declarations {
		if typeDecl, ok := decl.(*TypeDecl); ok && typeDecl.Name == name {
			return typeDecl
		}
	}
	return nil
}

// sortDeclarations sorts declarations for consistent output order
func (b *Builder) sortDeclarations() {
	sort.Slice(b.file.Declarations, func(i, j int) bool {
		// Define sort order: types first, then constants, then structs (with methods), then standalone methods
		orderI := getDeclarationOrder(b.file.Declarations[i])
		orderJ := getDeclarationOrder(b.file.Declarations[j])

		if orderI != orderJ {
			return orderI < orderJ
		}

		// Within same type, sort alphabetically by name
		nameI := getDeclarationName(b.file.Declarations[i])
		nameJ := getDeclarationName(b.file.Declarations[j])
		return nameI < nameJ
	})
}

// getDeclarationOrder returns sort order for declaration types
func getDeclarationOrder(decl Declaration) int {
	switch decl.(type) {
	case *TypeDecl:
		return 1
	case *ConstDecl, *ConstBlock:
		return 2
	case *StructDecl:
		return 3
	case *VariableDecl:
		return 4
	case *MethodDecl:
		return 5
	default:
		return 6
	}
}

// getDeclarationName extracts name from declaration for sorting
func getDeclarationName(decl Declaration) string {
	switch d := decl.(type) {
	case *TypeDecl:
		return d.Name
	case *ConstDecl:
		return d.Name
	case *ConstBlock:
		if len(d.Consts) > 0 {
			return d.Consts[0].Name
		}
		return ""
	case *StructDecl:
		return d.Name
	case *VariableDecl:
		return d.Name
	case *MethodDecl:
		return d.Name
	default:
		return ""
	}
}
