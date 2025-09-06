package string

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestMasking(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"foobar", "foo***"},
		{"foo", "f**"},
		{"f", "*"},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Mask(%q)", tc.input), func(t *testing.T) {
			output := Mask(tc.input)
			assert.Equal(t, tc.expected, output)
		})
	}
}

func TestMaskUrl(t *testing.T) {
	u, err := MaskURL("http://user:password@localhost:8080/path?query=1")
	assert.NoError(t, err)
	assert.Equal(t, "http://us**:pass****@localhost:8080/pa**?query=*", u)

	u, err = MaskURL("snowflake://FOO:thisisapassword@TFLXCJY-LU41011/TEST/PUBLIC")
	assert.NoError(t, err)
	assert.Equal(t, "snowflake://F**:thisisa********@TFLXCJY-LU41011/TEST/******", u)

	u, err = MaskURL("s3://bucket/folder?region=us-west-2&access-key-id=AKIAIOSFODNN7EXAMPLE&secret-access-key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	assert.NoError(t, err)
	assert.Equal(t, "s3://bucket/fol***?access-key-id=AKIAIOSFOD**********&region=us-w*****&secret-access-key=wJalrXUtnFEMI/K7MDEN********************", u)
}

func TestMaskArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "Mask URL",
			args: []string{"https://alice:bob@example.com/a/b?foo=bar", "http://user:password@localhost:8080/path?query=1", "s3://bucket/folder?region=us-west-2&access-key-id=AKIAIOSFODNN7EXAMPLE&secret-access", "mysql://user:password@localhost:3306/db?query=1"},
			want: []string{"https://al***:b**@example.com/a**?foo=b**", "http://us**:pass****@localhost:8080/pa**?query=*", "s3://bucket/fol***?access-key-id=AKIAIOSFOD**********&region=us-w*****&secret-access=", "mysql://us**:pass****@localhost:3306/d*?query=*"},
		},
		{
			name: "Mask Email",
			args: []string{"user@example.com", "another.user@example.com"},
			want: []string{"us**@exa****.com", "anothe******@exa****.com"},
		},
		{
			name: "Mask JWT",
			args: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"},
			want: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6Ikpv******************************************************************************"},
		},
		{
			name: "No Masking Needed",
			args: []string{"hello", "world"},
			want: []string{"hello", "world"},
		},
		{
			name: "Mixed Arguments",
			args: []string{"http://example.com", "user@example.com", "hello"},
			want: []string{"http://example.com", "us**@exa****.com", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskArguments(tt.args)
			assert.Equal(t, tt.want, got)
		})
	}

}

func TestMaskedEmail(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"test@example.com", "te**@exa****.com"},
		{"user@example.co.uk", "us**@exa****.co.uk"},
	}
	for _, test := range tests {
		result := MaskEmail(test.email)
		assert.Equal(t, test.expected, result)
	}
}

func TestMaskedString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"single char", "a", "*"},
		{"short string", "abc", "a**"},
		{"medium string", "password", "pass****"},
		{"long string", "verylongpassword", "verylong********"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMaskedString(tt.input)
			assert.Equal(t, tt.want, ms.String())
		})
	}
}

func TestMaskedString_MarshalText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"single char", "a", "*"},
		{"password", "secret123", "secr*****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMaskedString(tt.input)
			text, err := ms.MarshalText()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(text))
		})
	}
}

func TestMaskedString_MarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", `""`},
		{"password", "secret123", `"secret123"`},
		{"with quotes", `test"quote`, `"test\"quote"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMaskedString(tt.input)
			json, err := ms.MarshalJSON()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(json))
		})
	}
}

func TestMaskedString_MarshalYAML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"password", "secret123", "secret123"},
		{"with special chars", "test@value", "test@value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMaskedString(tt.input)
			yaml, err := ms.MarshalYAML()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, yaml)
		})
	}
}

func TestMaskedString_Text(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"single char", "a", "a"},
		{"password", "secret123", "secret123"},
		{"long text", "this is a very long secret password", "this is a very long secret password"},
		{"with special chars", "test@#$%^&*()", "test@#$%^&*()"},
		{"with quotes", `test"quote'mixed`, `test"quote'mixed`},
		{"with newlines", "line1\nline2\nline3", "line1\nline2\nline3"},
		{"unicode", "café🔒", "café🔒"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMaskedString(tt.input)
			text := ms.Text()
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestMaskedString_Behavior_Comparison(t *testing.T) {
	testCases := []string{
		"",
		"a",
		"password123",
		"very_long_secret_value_that_should_be_masked",
	}

	for _, input := range testCases {
		t.Run(fmt.Sprintf("input_%s", input), func(t *testing.T) {
			ms := NewMaskedString(input)

			// Text() should return unmasked original value
			assert.Equal(t, input, ms.Text())

			// String() should return masked value (unless empty)
			if input == "" {
				assert.Equal(t, "", ms.String())
			} else {
				assert.Equal(t, Mask(input), ms.String())
			}

			// JSON should serialize unmasked value
			jsonBytes, err := ms.MarshalJSON()
			assert.NoError(t, err)
			expectedJSON, _ := json.Marshal(input)
			assert.Equal(t, expectedJSON, jsonBytes)

			// YAML should return unmasked value
			yamlVal, err := ms.MarshalYAML()
			assert.NoError(t, err)
			assert.Equal(t, input, yamlVal)
		})
	}
}

type TestStruct struct {
	Value    MaskedString `json:"value" yaml:"value"`
	Name     string       `json:"name" yaml:"name"`
	Password MaskedString `json:"password" yaml:"password"`
}

func TestMaskedString_JSON_Unmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		expected TestStruct
	}{
		{
			name:    "basic values",
			jsonStr: `{"value":"secret123","name":"test","password":"mypassword"}`,
			expected: TestStruct{
				Value:    NewMaskedString("secret123"),
				Name:     "test",
				Password: NewMaskedString("mypassword"),
			},
		},
		{
			name:    "empty values",
			jsonStr: `{"value":"","name":"","password":""}`,
			expected: TestStruct{
				Value:    NewMaskedString(""),
				Name:     "",
				Password: NewMaskedString(""),
			},
		},
		{
			name:    "with special characters",
			jsonStr: `{"value":"test@#$%","name":"user","password":"p@ssw0rd!"}`,
			expected: TestStruct{
				Value:    NewMaskedString("test@#$%"),
				Name:     "user",
				Password: NewMaskedString("p@ssw0rd!"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result TestStruct
			err := json.Unmarshal([]byte(tt.jsonStr), &result)
			assert.NoError(t, err)

			// Check that values were unmarshaled correctly
			assert.Equal(t, tt.expected.Value.Text(), result.Value.Text())
			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Password.Text(), result.Password.Text())

			// Verify that String() still returns masked values
			if tt.expected.Value.Text() != "" {
				assert.Equal(t, Mask(tt.expected.Value.Text()), result.Value.String())
			}
			if tt.expected.Password.Text() != "" {
				assert.Equal(t, Mask(tt.expected.Password.Text()), result.Password.String())
			}
		})
	}
}

func TestMaskedString_YAML_Unmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		yamlStr  string
		expected TestStruct
	}{
		{
			name: "basic values",
			yamlStr: `value: secret123
name: test
password: mypassword`,
			expected: TestStruct{
				Value:    NewMaskedString("secret123"),
				Name:     "test",
				Password: NewMaskedString("mypassword"),
			},
		},
		{
			name: "empty values",
			yamlStr: `value: ""
name: ""
password: ""`,
			expected: TestStruct{
				Value:    NewMaskedString(""),
				Name:     "",
				Password: NewMaskedString(""),
			},
		},
		{
			name: "with special characters",
			yamlStr: `value: "test@#$%"
name: user
password: "p@ssw0rd!"`,
			expected: TestStruct{
				Value:    NewMaskedString("test@#$%"),
				Name:     "user",
				Password: NewMaskedString("p@ssw0rd!"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result TestStruct
			err := yaml.Unmarshal([]byte(tt.yamlStr), &result)
			assert.NoError(t, err)

			// Check that values were unmarshaled correctly
			assert.Equal(t, tt.expected.Value.Text(), result.Value.Text())
			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Password.Text(), result.Password.Text())

			// Verify that String() still returns masked values
			if tt.expected.Value.Text() != "" {
				assert.Equal(t, Mask(tt.expected.Value.Text()), result.Value.String())
			}
			if tt.expected.Password.Text() != "" {
				assert.Equal(t, Mask(tt.expected.Password.Text()), result.Password.String())
			}
		})
	}
}

func TestMaskedString_RoundTrip_JSON(t *testing.T) {
	original := TestStruct{
		Value:    NewMaskedString("secret_value"),
		Name:     "testuser",
		Password: NewMaskedString("super_secret_password"),
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled TestStruct
	err = json.Unmarshal(jsonData, &unmarshaled)
	assert.NoError(t, err)

	// Verify round-trip worked correctly
	assert.Equal(t, original.Value.Text(), unmarshaled.Value.Text())
	assert.Equal(t, original.Name, unmarshaled.Name)
	assert.Equal(t, original.Password.Text(), unmarshaled.Password.Text())

	// Verify masking still works
	assert.Equal(t, original.Value.String(), unmarshaled.Value.String())
	assert.Equal(t, original.Password.String(), unmarshaled.Password.String())
}

func TestMaskedString_RoundTrip_YAML(t *testing.T) {
	original := TestStruct{
		Value:    NewMaskedString("secret_value"),
		Name:     "testuser",
		Password: NewMaskedString("super_secret_password"),
	}

	// Marshal to YAML
	yamlData, err := yaml.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled TestStruct
	err = yaml.Unmarshal(yamlData, &unmarshaled)
	assert.NoError(t, err)

	// Verify round-trip worked correctly
	assert.Equal(t, original.Value.Text(), unmarshaled.Value.Text())
	assert.Equal(t, original.Name, unmarshaled.Name)
	assert.Equal(t, original.Password.Text(), unmarshaled.Password.Text())

	// Verify masking still works
	assert.Equal(t, original.Value.String(), unmarshaled.Value.String())
	assert.Equal(t, original.Password.String(), unmarshaled.Password.String())
}
