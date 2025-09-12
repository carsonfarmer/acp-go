package codegen

import (
	"strings"
	"testing"
)

// Tests for the Builder API - CreateStruct, CreateType, CreateConstBlock, CreateField, WithField, and chaining methods

func TestBuilderAPI_CreateType(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test CreateType with different type declarations
	tests := []struct {
		name     string
		typeName string
		goType   string
		expected string
	}{
		{
			name:     "simple string type",
			typeName: "UserID",
			goType:   "string",
			expected: "type UserID string",
		},
		{
			name:     "pointer type",
			typeName: "OptionalUser",
			goType:   "*User",
			expected: "type OptionalUser *User",
		},
		{
			name:     "slice type",
			typeName: "UserList",
			goType:   "[]User",
			expected: "type UserList []User",
		},
		{
			name:     "map type",
			typeName: "UserMap",
			goType:   "map[string]User",
			expected: "type UserMap map[string]User",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeDecl := builder.CreateType(tt.typeName, tt.goType)
			
			if typeDecl == nil {
				t.Fatal("CreateType returned nil")
			}
			
			if typeDecl.Name != tt.typeName {
				t.Errorf("Expected type name %s, got %s", tt.typeName, typeDecl.Name)
			}
			
			if typeDecl.Type.String() != tt.goType {
				t.Errorf("Expected type %s, got %s", tt.goType, typeDecl.Type.String())
			}
		})
	}
	
	// Build and check output
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	for _, tt := range tests {
		if !strings.Contains(generatedStr, tt.expected) {
			t.Errorf("Expected type declaration %s in output", tt.expected)
		}
	}
}

func TestBuilderAPI_CreateConstBlock(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test CreateConstBlock
	constBlock := builder.CreateConstBlock()
	
	if constBlock == nil {
		t.Fatal("CreateConstBlock returned nil")
	}
	
	if len(constBlock.Consts) != 0 {
		t.Errorf("Expected empty const block, got %d constants", len(constBlock.Consts))
	}
	
	// Add some constants to the block
	constBlock.AddConst("MaxUsers", "int", "100", "Maximum number of users")
	constBlock.AddConst("DefaultTimeout", "time.Duration", "30*time.Second", "Default timeout")
	
	if len(constBlock.Consts) != 2 {
		t.Errorf("Expected 2 constants, got %d", len(constBlock.Consts))
	}
	
	// Check first constant
	if constBlock.Consts[0].Name != "MaxUsers" {
		t.Errorf("Expected first constant name 'MaxUsers', got %s", constBlock.Consts[0].Name)
	}
	
	// Build and check output
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	if !strings.Contains(generatedStr, "const (") {
		t.Error("Expected const block in output")
	}
	if !strings.Contains(generatedStr, "MaxUsers") {
		t.Error("Expected MaxUsers constant in output")
	}
	if !strings.Contains(generatedStr, "DefaultTimeout") {
		t.Error("Expected DefaultTimeout constant in output")
	}
}

func TestBuilderAPI_CreateStruct(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test new API: builder.CreateStruct()
	st := builder.CreateStruct("Person")
	
	if st == nil {
		t.Fatal("CreateStruct returned nil")
	}
	
	if st.Name != "Person" {
		t.Errorf("Expected struct name 'Person', got %s", st.Name)
	}
	
	// Build and check output
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	if !strings.Contains(generatedStr, "type Person struct") {
		t.Error("Expected struct declaration in output")
	}
}

func TestBuilderAPI_CreateField(t *testing.T) {
	builder := NewBuilder("example")
	st := builder.CreateStruct("User")
	
	// Test CreateField with different field types
	tests := []struct {
		name      string
		fieldName string
		fieldType string
		fieldTag  string
		wantName  string
		wantType  string
		wantTag   string
	}{
		{
			name:      "simple field",
			fieldName: "Name",
			fieldType: "string",
			wantName:  "Name",
			wantType:  "string",
		},
		{
			name:      "field with tag",
			fieldName: "Email",
			fieldType: "string",
			fieldTag:  `json:"email"`,
			wantName:  "Email",
			wantType:  "string",
			wantTag:   `json:"email"`,
		},
		{
			name:      "pointer field",
			fieldName: "Profile",
			fieldType: "*UserProfile",
			wantName:  "Profile",
			wantType:  "*UserProfile",
		},
		{
			name:      "slice field",
			fieldName: "Tags",
			fieldType: "[]string",
			wantName:  "Tags",
			wantType:  "[]string",
		},
		{
			name:      "map field",
			fieldName: "Metadata",
			fieldType: "map[string]interface{}",
			wantName:  "Metadata",
			wantType:  "map[string]interface{}",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var field *Field
			var err error
			
			if tt.fieldTag != "" {
				field, err = st.CreateField(tt.fieldName, tt.fieldType, tt.fieldTag)
			} else {
				field, err = st.CreateField(tt.fieldName, tt.fieldType)
			}
			
			if err != nil {
				t.Fatalf("CreateField failed: %v", err)
			}
			
			if field.Name != tt.wantName {
				t.Errorf("Expected field name %s, got %s", tt.wantName, field.Name)
			}
			
			if field.Type.String() != tt.wantType {
				t.Errorf("Expected field type %s, got %s", tt.wantType, field.Type.String())
			}
			
			if field.Tag != tt.wantTag {
				t.Errorf("Expected field tag %s, got %s", tt.wantTag, field.Tag)
			}
		})
	}
	
	// Check if all fields were added to struct
	if len(st.Fields) != len(tests) {
		t.Errorf("Expected %d fields in struct, got %d", len(tests), len(st.Fields))
	}
}

func TestBuilderAPI_CreateMethod(t *testing.T) {
	builder := NewBuilder("example")
	st := builder.CreateStruct("Calculator")
	
	// Add some fields first using new API
	st.CreateField("Value", "int")
	
	// Test CreateMethod (already implemented)
	methodDef := `func (c *Calculator) Add(n int) int {
c.Value += n
return c.Value
}`
	
	method, err := st.CreateMethod(methodDef)
	if err != nil {
		t.Fatalf("CreateMethod failed: %v", err)
	}
	
	if method.Name != "Add" {
		t.Errorf("Expected method name 'Add', got %s", method.Name)
	}
	
	if len(method.Params) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(method.Params))
	}
	
	if len(method.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(method.Results))
	}
	
	// Check if method was added to struct
	if len(st.Methods) != 1 {
		t.Errorf("Expected 1 method in struct, got %d", len(st.Methods))
	}
}

func TestBuilderAPI_WithField(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test chaining with simple field creation
	st := builder.CreateStruct("Product").
		WithField("ID", "int64", `json:"id" db:"id"`).
		WithField("Name", "string", `json:"name"`).
		WithField("Price", "float64").
		WithField("Tags", "[]string", `json:"tags"`).
		WithMethod(`func (p *Product) GetID() int64 { return p.ID }`)
	
	// Verify fields
	if len(st.Fields) != 4 {
		t.Errorf("Expected 4 fields, got %d", len(st.Fields))
	}
	
	// Check specific fields
	fields := st.Fields
	
	if fields[0].Name != "ID" || fields[0].Type.String() != "int64" {
		t.Errorf("ID field incorrect: name=%s, type=%s", fields[0].Name, fields[0].Type.String())
	}
	
	expectedIDTag := `db:"id" json:"id"` // 태그가 알파벳 순으로 정렬됨
	if fields[0].Tag != expectedIDTag {
		t.Errorf("ID field tag expected %s, got %s", expectedIDTag, fields[0].Tag)
	}
	
	if fields[1].Name != "Name" || fields[1].Type.String() != "string" {
		t.Errorf("Name field incorrect: name=%s, type=%s", fields[1].Name, fields[1].Type.String())
	}
	
	if fields[2].Name != "Price" || fields[2].Type.String() != "float64" {
		t.Errorf("Price field incorrect: name=%s, type=%s", fields[2].Name, fields[2].Type.String())
	}
	
	if fields[3].Name != "Tags" || fields[3].Type.String() != "[]string" {
		t.Errorf("Tags field incorrect: name=%s, type=%s", fields[3].Name, fields[3].Type.String())
	}
	
	// Verify method
	if len(st.Methods) != 1 {
		t.Errorf("Expected 1 method, got %d", len(st.Methods))
	}
}

func TestBuilderAPI_Chaining(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test method chaining with WithField and WithMethod
	st := builder.CreateStruct("Product").
		WithField("ID", "int64", `json:"id"`).
		WithField("Name", "string", `json:"name"`).
		WithField("Price", "float64", `json:"price"`).
		WithMethod(`func (p *Product) GetID() int64 {
			return p.ID
		}`).
		WithMethod(`func (p *Product) SetPrice(price float64) error {
			if price < 0 {
				return fmt.Errorf("price cannot be negative")
			}
			p.Price = price
			return nil
		}`)
	
	// Verify the struct has the expected fields and methods
	if len(st.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(st.Fields))
	}
	
	if len(st.Methods) != 2 {
		t.Errorf("Expected 2 methods, got %d", len(st.Methods))
	}
	
	// Build and verify output
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	
	// Check struct
	if !strings.Contains(generatedStr, "type Product struct") {
		t.Error("Expected Product struct definition")
	}
	
	// Check fields
	expectedFields := []string{"ID", "Name", "Price"}
	for _, field := range expectedFields {
		if !strings.Contains(generatedStr, field) {
			t.Errorf("Expected field %s in output", field)
		}
	}
	
	// Check methods
	if !strings.Contains(generatedStr, "func (p *Product) GetID() int64") {
		t.Error("Expected GetID method")
	}
	
	if !strings.Contains(generatedStr, "func (p *Product) SetPrice(price float64) error") {
		t.Error("Expected SetPrice method")
	}
}

func TestBuilderAPI_MultipleStructs(t *testing.T) {
	builder := NewBuilder("ecommerce")
	
	// Create multiple structs with chaining
	builder.CreateStruct("User").
		WithField("ID", "int64", `json:"id"`).
		WithField("Username", "string", `json:"username"`).
		WithMethod(`func (u *User) GetUsername() string { return u.Username }`)
	
	builder.CreateStruct("Order").
		WithField("ID", "int64", `json:"id"`).
		WithField("UserID", "int64", `json:"user_id"`).
		WithField("Total", "float64", `json:"total"`).
		WithMethod(`func (o *Order) GetTotal() float64 { return o.Total }`)
	
	// Build and verify
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	
	// Check both structs exist
	if !strings.Contains(generatedStr, "type User struct") {
		t.Error("Expected User struct definition")
	}
	
	if !strings.Contains(generatedStr, "type Order struct") {
		t.Error("Expected Order struct definition")
	}
	
	// Check methods for both
	if !strings.Contains(generatedStr, "func (u *User) GetUsername() string") {
		t.Error("Expected GetUsername method")
	}
	
	if !strings.Contains(generatedStr, "func (o *Order) GetTotal() float64") {
		t.Error("Expected GetTotal method")
	}
}

func TestBuilderAPI_ErrorHandling(t *testing.T) {
	builder := NewBuilder("test")
	st := builder.CreateStruct("TestStruct")
	
	// Test with empty field name - this should work fine in the new API
	// (since we're testing chaining, we just verify it doesn't break)
	result := st.WithField("ValidField", "string", "")
	if result != st {
		t.Error("WithField should return the same struct for chaining")
	}
	
	// Verify field was added
	if len(st.Fields) != 1 {
		t.Errorf("Expected 1 field, got %d", len(st.Fields))
	}
}

func TestBuilderAPI_Comparison(t *testing.T) {
	// Compare old vs new API
	
	// Verbose way (multiple calls)
	builder1 := NewBuilder("old")
	st1 := builder1.CreateStruct("Person")
	field1, err := st1.CreateField("Name", "string", `json:"name"`)
	if err != nil {
		t.Fatal(err)
	}
	_ = field1
	
	field2, err := st1.CreateField("Age", "int", `json:"age"`)
	if err != nil {
		t.Fatal(err)
	}
	_ = field2
	
	method1, err := st1.CreateMethod(`func (p *Person) GetName() string { return p.Name }`)
	if err != nil {
		t.Fatal(err)
	}
	_ = method1
	
	// Chained way (method chaining)
	builder2 := NewBuilder("new")
	builder2.CreateStruct("Person").
		WithField("Name", "string", `json:"name"`).
		WithField("Age", "int", `json:"age"`).
		WithMethod(`func (p *Person) GetName() string { return p.Name }`)
	
	// Both should produce the same result
	generated1, err1 := builder1.Build()
	generated2, err2 := builder2.Build()
	
	if err1 != nil || err2 != nil {
		t.Fatalf("Build failed: %v, %v", err1, err2)
	}
	
	// Replace package names for comparison
	str1 := strings.Replace(string(generated1), "package old", "package test", 1)
	str2 := strings.Replace(string(generated2), "package new", "package test", 1)
	
	if str1 != str2 {
		t.Errorf("Different outputs:\nOld:\n%s\n\nNew:\n%s", str1, str2)
	}
}

func TestBuilderAPI_IntegratedExample(t *testing.T) {
	builder := NewBuilder("demo")
	
	// Create types using new CreateType method
	userIDType := builder.CreateType("UserID", "string")
	if userIDType.Name != "UserID" {
		t.Errorf("Expected type name UserID, got %s", userIDType.Name)
	}
	
	// Create const block using new CreateConstBlock method
	constants := builder.CreateConstBlock()
	constants.AddConst("MaxUsers", "int", "1000", "Maximum number of users allowed")
	constants.AddConst("DefaultPageSize", "int", "50", "Default pagination size")
	
	// Create struct using existing CreateStruct method 
	builder.CreateStruct("User").
		WithField("ID", "UserID", `json:"id" db:"id"`).
		WithField("Name", "string", `json:"name" validate:"required"`).
		WithField("Email", "string", `json:"email" validate:"email,required"`)
	
	// Build and verify
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	
	// Verify types
	if !strings.Contains(generatedStr, "type UserID string") {
		t.Error("Expected UserID type definition")
	}
	
	// Verify constants  
	if !strings.Contains(generatedStr, "const (") {
		t.Error("Expected const block")
	}
	if !strings.Contains(generatedStr, "MaxUsers int = 1000") {
		t.Error("Expected MaxUsers constant")
	}
	if !strings.Contains(generatedStr, "DefaultPageSize int = 50") {
		t.Error("Expected DefaultPageSize constant")
	}
	
	// Verify struct
	if !strings.Contains(generatedStr, "type User struct") {
		t.Error("Expected User struct")
	}
	if !strings.Contains(generatedStr, "ID") || !strings.Contains(generatedStr, "UserID") {
		t.Error("Expected ID field with UserID type")
	}
	
	// Verify order: types first, then constants, then structs
	userIDPos := strings.Index(generatedStr, "type UserID")
	constPos := strings.Index(generatedStr, "const (")
	structPos := strings.Index(generatedStr, "type User struct")
	
	if !(userIDPos < constPos && constPos < structPos) {
		t.Errorf("Incorrect declaration order. UserID: %d, const: %d, struct: %d", 
			userIDPos, constPos, structPos)
	}
}

func TestBuilderAPI_CompleteExample(t *testing.T) {
	builder := NewBuilder("models")
	
	// Create a complete model using the Builder API
	user := builder.CreateStruct("User")
	
	// Add fields using the simple API
	user.CreateField("ID", "int64", `json:"id" db:"id"`)
	user.CreateField("Username", "string", `json:"username" db:"username"`)
	user.CreateField("Email", "string", `json:"email" db:"email"`)
	user.CreateField("Profile", "*UserProfile", `json:"profile,omitempty"`)
	user.CreateField("Tags", "[]string", `json:"tags" db:"tags"`)
	user.CreateField("Metadata", "map[string]interface{}", `json:"metadata,omitempty"`)
	
	// Add methods
	user.CreateMethod(`func (u *User) GetID() int64 { return u.ID }`)
	user.CreateMethod(`func (u *User) SetEmail(email string) error {
		if email == "" {
			return fmt.Errorf("email cannot be empty")
		}
		u.Email = email
		return nil
	}`)
	
	// Build and verify
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	
	// Check struct exists
	if !strings.Contains(generatedStr, "type User struct") {
		t.Error("Expected User struct definition")
	}
	
	// Check all field types are correct
	expectedFields := []string{"ID", "Username", "Email", "Profile", "Tags", "Metadata"}
	
	for _, fieldName := range expectedFields {
		if !strings.Contains(generatedStr, fieldName) {
			t.Errorf("Expected field %s in output", fieldName)
		}
	}
	
	// Check tags exist
	expectedTags := []string{
		`json:"id"`,
		`db:"id"`,
		`json:"username"`,
		`json:"email"`,
		`json:"tags"`,
	}
	
	for _, tag := range expectedTags {
		if !strings.Contains(generatedStr, tag) {
			t.Errorf("Expected tag %s in output", tag)
		}
	}
	
	// Check methods
	if !strings.Contains(generatedStr, "func (u *User) GetID() int64") {
		t.Error("Expected GetID method")
	}
	
	if !strings.Contains(generatedStr, "func (u *User) SetEmail(email string) error") {
		t.Error("Expected SetEmail method")
	}
}