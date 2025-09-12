package codegen

import (
	"fmt"
	"strings"
	"testing"
)

func TestMultiTag_CreateField(t *testing.T) {
	builder := NewBuilder("example")
	st := builder.CreateStruct("User")
	
	tests := []struct {
		name      string
		fieldName string
		fieldType string
		tags      []string
		wantTag   string
	}{
		{
			name:      "single tag",
			fieldName: "Name",
			fieldType: "string",
			tags:      []string{`json:"name"`},
			wantTag:   `json:"name"`,
		},
		{
			name:      "multiple different tags",
			fieldName: "Email",
			fieldType: "string",
			tags:      []string{`json:"email"`, `db:"email"`},
			wantTag:   `db:"email" json:"email"`, // sorted alphabetically
		},
		{
			name:      "duplicate tag keys merged",
			fieldName: "ID",
			fieldType: "int64",
			tags:      []string{`json:"id"`, `json:"primary"`, `db:"id"`},
			wantTag:   `db:"id" json:"id,primary"`,
		},
		{
			name:      "complex multiple tags",
			fieldName: "Status",
			fieldType: "string",
			tags:      []string{`json:"status"`, `json:"state"`, `db:"status"`, `validate:"required"`},
			wantTag:   `db:"status" json:"status,state" validate:"required"`,
		},
		{
			name:      "tags with spaces",
			fieldName: "Data",
			fieldType: "map[string]interface{}",
			tags:      []string{`json:"data"`, `bson:"data,omitempty"`},
			wantTag:   `bson:"data,omitempty" json:"data"`,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, err := st.CreateField(tt.fieldName, tt.fieldType, tt.tags...)
			if err != nil {
				t.Fatalf("CreateField failed: %v", err)
			}
			
			if field.Name != tt.fieldName {
				t.Errorf("Expected field name %s, got %s", tt.fieldName, field.Name)
			}
			
			if field.Tag != tt.wantTag {
				t.Errorf("Expected field tag %s, got %s", tt.wantTag, field.Tag)
			}
		})
	}
}

func TestMultiTag_WithField(t *testing.T) {
	builder := NewBuilder("example")
	
	// Test chaining with multiple tags
	st := builder.CreateStruct("Product").
		WithField("ID", "int64", `json:"id"`, `json:"primary"`, `db:"id"`).
		WithField("Name", "string", `json:"name"`, `db:"name"`, `validate:"required"`).
		WithField("Price", "float64", `json:"price"`, `db:"price"`)
	
	// Verify fields
	if len(st.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(st.Fields))
	}
	
	// Check ID field with merged json tags
	idField := st.Fields[0]
	if idField.Name != "ID" {
		t.Errorf("Expected ID field, got %s", idField.Name)
	}
	expectedIDTag := `db:"id" json:"id,primary"`
	if idField.Tag != expectedIDTag {
		t.Errorf("Expected ID tag %s, got %s", expectedIDTag, idField.Tag)
	}
	
	// Check Name field with multiple different tags
	nameField := st.Fields[1]
	if nameField.Name != "Name" {
		t.Errorf("Expected Name field, got %s", nameField.Name)
	}
	expectedNameTag := `db:"name" json:"name" validate:"required"`
	if nameField.Tag != expectedNameTag {
		t.Errorf("Expected Name tag %s, got %s", expectedNameTag, nameField.Tag)
	}
}

func TestMultiTag_TagParsing(t *testing.T) {
	builder := NewBuilder("test")
	st := builder.CreateStruct("TestStruct")
	
	// Test various tag formats
	tests := []struct {
		name     string
		tags     []string
		expected string
	}{
		{
			name:     "backticks handled",
			tags:     []string{"`json:\"name\"`", "`db:\"name\"`"},
			expected: `db:"name" json:"name"`,
		},
		{
			name:     "mixed formats",
			tags:     []string{`json:"id"`, "`db:\"id\"`", `validate:"required"`},
			expected: `db:"id" json:"id" validate:"required"`,
		},
		{
			name:     "empty tags filtered",
			tags:     []string{`json:"test"`, "", `db:"test"`},
			expected: `db:"test" json:"test"`,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, err := st.CreateField("TestField", "string", tt.tags...)
			if err != nil {
				t.Fatalf("CreateField failed: %v", err)
			}
			
			if field.Tag != tt.expected {
				t.Errorf("Expected tag %s, got %s", tt.expected, field.Tag)
			}
		})
	}
}

func TestMultiTag_GeneratedCode(t *testing.T) {
	builder := NewBuilder("models")
	
	// Create a struct with multi-tag fields
	user := builder.CreateStruct("User")
	user.CreateField("ID", "int64", `json:"id"`, `json:"primary"`, `db:"user_id"`, `validate:"required"`)
	user.CreateField("Username", "string", `json:"username"`, `json:"login"`, `db:"username"`)
	user.CreateField("Email", "string", `json:"email"`, `db:"email"`, `validate:"email,required"`)
	
	// Build and verify
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	generatedStr := string(generated)
	
	// Check that merged tags appear in output
	expectedTags := []string{
		`json:"id,primary"`,
		`db:"user_id"`,
		`validate:"required"`,
		`json:"username,login"`,
		`json:"email"`,
		`validate:"email,required"`,
	}
	
	for _, tag := range expectedTags {
		if !strings.Contains(generatedStr, tag) {
			t.Errorf("Expected tag %s not found in output:\n%s", tag, generatedStr)
		}
	}
}

func TestMultiTag_EdgeCases(t *testing.T) {
	builder := NewBuilder("test")
	st := builder.CreateStruct("TestStruct")
	
	// Test edge cases
	tests := []struct {
		name     string
		tags     []string
		expected string
	}{
		{
			name:     "no tags",
			tags:     []string{},
			expected: "",
		},
		{
			name:     "only empty tags",
			tags:     []string{"", "", ""},
			expected: "",
		},
		{
			name:     "duplicate identical tags",
			tags:     []string{`json:"name"`, `json:"name"`, `json:"name"`},
			expected: `json:"name,name,name"`, // Currently doesn't dedupe, which is fine
		},
		{
			name:     "malformed tag ignored",
			tags:     []string{`json:"valid"`, "malformed", `db:"valid"`},
			expected: `db:"valid" json:"valid"`,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, err := st.CreateField("TestField", "string", tt.tags...)
			if err != nil {
				t.Fatalf("CreateField failed: %v", err)
			}
			
			if field.Tag != tt.expected {
				t.Errorf("Expected tag %s, got %s", tt.expected, field.Tag)
			}
		})
	}
}

func TestMultiTag_Demo(t *testing.T) {
	builder := NewBuilder("models")
	
	// 다중 태그 기능 데모
	user := builder.CreateStruct("User")
	
	// 다양한 다중 태그 사용 예시
	user.CreateField("ID", "int64", 
		`json:"id"`, 
		`json:"primary"`, 
		`db:"user_id"`, 
		`validate:"required"`)
		
	user.CreateField("Username", "string", 
		`json:"username"`, 
		`json:"login"`, 
		`db:"username"`, 
		`validate:"required,min=3,max=50"`)
		
	user.CreateField("Email", "string", 
		`json:"email"`, 
		`db:"email"`, 
		`validate:"email,required"`, 
		`gorm:"uniqueIndex"`)
		
	user.CreateField("Profile", "*UserProfile", 
		`json:"profile,omitempty"`, 
		`db:"profile_id"`, 
		`gorm:"foreignKey:ProfileID"`)
		
	user.CreateField("Tags", "[]string", 
		`json:"tags"`, 
		`json:"categories"`, 
		`db:"tags"`, 
		`gorm:"type:text[]"`)

	// 체이닝 방식으로도 가능
	builder.CreateStruct("Order").
		WithField("ID", "int64", `json:"id"`, `json:"order_id"`, `db:"order_id"`).
		WithField("UserID", "int64", `json:"user_id"`, `db:"user_id"`, `gorm:"index"`).
		WithField("Status", "OrderStatus", `json:"status"`, `db:"status"`, `validate:"required"`).
		WithField("Total", "float64", `json:"total"`, `json:"amount"`, `db:"total"`, `validate:"min=0"`)
	
	// 생성된 코드 출력
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	
	fmt.Printf("=== 다중 태그 기능 데모 ===\n\n")
	fmt.Printf("다음과 같은 코드로:\n")
	fmt.Printf(`user.CreateField("ID", "int64", 
    json:"id", 
    json:"primary", 
    db:"user_id", 
    validate:"required")
    
user.CreateField("Username", "string", 
    json:"username", 
    json:"login", 
    db:"username", 
    validate:"required,min=3,max=50")

`)
	
	fmt.Printf("다음과 같은 Go 코드가 생성됩니다:\n\n")
	fmt.Printf("%s\n", string(generated))
	
	fmt.Printf("=== 주요 특징 ===\n")
	fmt.Printf("✅ 동일한 태그 키의 값들은 콤마로 병합: json:\"id,primary\"\n")
	fmt.Printf("✅ 서로 다른 태그 키들은 공백으로 분리: json:\"...\" db:\"...\" validate:\"...\"\n")
	fmt.Printf("✅ 태그 키들은 알파벳 순으로 정렬되어 일관성 보장\n")
	fmt.Printf("✅ 빈 태그는 자동으로 필터링\n")
	fmt.Printf("✅ 백틱과 따옴표 형식 모두 지원\n")
}