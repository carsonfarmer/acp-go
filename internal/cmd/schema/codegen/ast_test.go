package codegen

import (
	"strings"
	"testing"
)

func TestStructDecl_CreateMethod(t *testing.T) {
	tests := []struct {
		name       string
		methodDef  string
		wantName   string
		wantError  bool
		wantParams int
		wantResults int
	}{
		{
			name: "simple method",
			methodDef: `func (s *MyStruct) GetValue() string {
	return s.value
}`,
			wantName:    "GetValue",
			wantError:   false,
			wantParams:  0,
			wantResults: 1,
		},
		{
			name: "method with parameters",
			methodDef: `func (s *MyStruct) SetValue(value string, index int) error {
	s.value = value
	return nil
}`,
			wantName:    "SetValue",
			wantError:   false,
			wantParams:  2,
			wantResults: 1,
		},
		{
			name: "method with multiple return values",
			methodDef: `func (s *MyStruct) Process() (string, error) {
	return "result", nil
}`,
			wantName:    "Process",
			wantError:   false,
			wantParams:  0,
			wantResults: 2,
		},
		{
			name: "invalid syntax",
			methodDef: `func (s *MyStruct GetValue() string {
	return s.value
}`,
			wantError: true,
		},
		{
			name: "not a method",
			methodDef: `func GetValue() string {
	return "value"
}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd := &StructDecl{
				Name:    "MyStruct",
				Fields:  []*Field{},
				Methods: []*MethodDecl{},
			}

			method, err := sd.CreateMethod(tt.methodDef)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("CreateMethod() expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Errorf("CreateMethod() unexpected error: %v", err)
				return
			}

			if method.Name != tt.wantName {
				t.Errorf("CreateMethod() method name = %v, want %v", method.Name, tt.wantName)
			}

			if len(method.Params) != tt.wantParams {
				t.Errorf("CreateMethod() params count = %v, want %v", len(method.Params), tt.wantParams)
			}

			if len(method.Results) != tt.wantResults {
				t.Errorf("CreateMethod() results count = %v, want %v", len(method.Results), tt.wantResults)
			}

			// Check if method was added to struct
			if len(sd.Methods) != 1 {
				t.Errorf("CreateMethod() methods count in struct = %v, want 1", len(sd.Methods))
			}

			// Check if method body is extracted
			if method.Body == "" {
				t.Errorf("CreateMethod() method body is empty")
			}
		})
	}
}

func TestStructDecl_ExtractMethodBody(t *testing.T) {
	tests := []struct {
		name     string
		methodDef string
		wantBody  string
	}{
		{
			name: "simple body",
			methodDef: `func (s *MyStruct) GetValue() string {
	return s.value
}`,
			wantBody: "return s.value",
		},
		{
			name: "multiline body",
			methodDef: `func (s *MyStruct) Process() error {
	if s.value == "" {
		return fmt.Errorf("empty value")
	}
	s.processed = true
	return nil
}`,
			wantBody: `if s.value == "" {
		return fmt.Errorf("empty value")
	}
	s.processed = true
	return nil`,
		},
		{
			name: "nested braces",
			methodDef: `func (s *MyStruct) ProcessMap() {
	for key, value := range s.data {
		if value > 0 {
			s.result[key] = value * 2
		}
	}
}`,
			wantBody: `for key, value := range s.data {
		if value > 0 {
			s.result[key] = value * 2
		}
	}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd := &StructDecl{}
			body := sd.extractMethodBody(tt.methodDef)

			// Normalize whitespace for comparison
			gotBody := strings.TrimSpace(body)
			wantBody := strings.TrimSpace(tt.wantBody)

			if gotBody != wantBody {
				t.Errorf("extractMethodBody() = %q, want %q", gotBody, wantBody)
			}
		})
	}
}