package utils

import (
	"strings"
	"testing"
)

func TestResponseParser_ExtractJSON_Valid(t *testing.T) {
	content := `{"key": "value", "number": 42}`
	parser := NewResponseParser(content)

	result, err := parser.ExtractJSON()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != content {
		t.Errorf("Expected %s, got: %s", content, result)
	}
}

func TestResponseParser_ExtractJSON_CodeBlock(t *testing.T) {
	content := "```json\n{\"key\": \"value\"}\n```"
	parser := NewResponseParser(content)

	result, err := parser.ExtractJSON()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("Expected result to contain JSON, got: %s", result)
	}
}

func TestResponseParser_ExtractJSON_Braces(t *testing.T) {
	content := `Some text before {"key": "value"} some text after`
	parser := NewResponseParser(content)

	result, err := parser.ExtractJSON()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("Expected result to contain JSON")
	}
}

func TestResponseParser_ExtractJSON_NoJSON(t *testing.T) {
	content := "This is just plain text with no JSON"
	parser := NewResponseParser(content)

	_, err := parser.ExtractJSON()
	if err == nil {
		t.Error("Expected error for content without JSON")
	}
}

func TestResponseParser_ParseToMap(t *testing.T) {
	content := `{"name": "test", "value": 123}`
	parser := NewResponseParser(content)

	result, err := parser.ParseToMap()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("Expected name to be 'test', got: %v", result["name"])
	}
	if result["value"].(float64) != 123 {
		t.Errorf("Expected value to be 123, got: %v", result["value"])
	}
}

func TestResponseParser_ParseToMap_Invalid(t *testing.T) {
	content := "not json"
	parser := NewResponseParser(content)

	_, err := parser.ParseToMap()
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestResponseParser_ParseToStruct(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	content := `{"name": "test", "value": 42}`
	parser := NewResponseParser(content)

	var result TestStruct
	err := parser.ParseToStruct(&result)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result.Name != "test" {
		t.Errorf("Expected name to be 'test', got: %s", result.Name)
	}
	if result.Value != 42 {
		t.Errorf("Expected value to be 42, got: %d", result.Value)
	}
}

func TestResponseParser_ExtractCodeBlock(t *testing.T) {
	content := "```go\nfunc main() {}\n```"
	parser := NewResponseParser(content)

	result, err := parser.ExtractCodeBlock("go")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !strings.Contains(result, "func main") {
		t.Errorf("Expected result to contain code, got: %s", result)
	}
}

func TestResponseParser_ExtractCodeBlock_NotFound(t *testing.T) {
	content := "```go\nfunc main() {}\n```"
	parser := NewResponseParser(content)

	_, err := parser.ExtractCodeBlock("python")
	if err == nil {
		t.Error("Expected error when code block not found")
	}
}

func TestResponseParser_ExtractAllCodeBlocks(t *testing.T) {
	content := "```go\nfunc main() {}\n```\n\n```python\nprint('hello')\n```"
	parser := NewResponseParser(content)

	result := parser.ExtractAllCodeBlocks()

	if len(result) != 2 {
		t.Errorf("Expected 2 code blocks, got: %d", len(result))
	}
	if _, ok := result["go"]; !ok {
		t.Error("Expected 'go' code block")
	}
	if _, ok := result["python"]; !ok {
		t.Error("Expected 'python' code block")
	}
}

func TestResponseParser_ExtractList_Numbered(t *testing.T) {
	content := `1. First item
2. Second item
3. Third item`
	parser := NewResponseParser(content)

	result := parser.ExtractList()

	if len(result) != 3 {
		t.Errorf("Expected 3 items, got: %d", len(result))
	}
	if result[0] != "First item" {
		t.Errorf("Expected 'First item', got: %s", result[0])
	}
}

func TestResponseParser_ExtractList_Bullets(t *testing.T) {
	content := `- First item
- Second item
* Third item`
	parser := NewResponseParser(content)

	result := parser.ExtractList()

	if len(result) != 3 {
		t.Errorf("Expected 3 items, got: %d", len(result))
	}
}

func TestResponseParser_ExtractKeyValue_FromJSON(t *testing.T) {
	content := `{"name": "John", "age": "30"}`
	parser := NewResponseParser(content)

	result, err := parser.ExtractKeyValue("name")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != "John" {
		t.Errorf("Expected 'John', got: %s", result)
	}
}

func TestResponseParser_ExtractKeyValue_FromText(t *testing.T) {
	content := `name: John Doe
age: 30`
	parser := NewResponseParser(content)

	result, err := parser.ExtractKeyValue("name")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !strings.Contains(result, "John") {
		t.Errorf("Expected result to contain 'John', got: %s", result)
	}
}

func TestResponseParser_ExtractKeyValue_NotFound(t *testing.T) {
	content := "Some random text"
	parser := NewResponseParser(content)

	_, err := parser.ExtractKeyValue("nonexistent")
	if err == nil {
		t.Error("Expected error when key not found")
	}
}

func TestResponseParser_ExtractKeyValue_NonString(t *testing.T) {
	content := `{"name": 123}`
	parser := NewResponseParser(content)

	_, err := parser.ExtractKeyValue("name")
	if err == nil {
		t.Error("Expected error for non-string value")
	}
}

func TestResponseParser_ExtractSection(t *testing.T) {
	content := `# Introduction
This is intro

## Details
This is details

### Subsection
More info`
	parser := NewResponseParser(content)

	result, err := parser.ExtractSection("Details")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !strings.Contains(result, "This is details") {
		t.Errorf("Expected section content, got: %s", result)
	}
}

func TestResponseParser_ExtractSection_NotFound(t *testing.T) {
	content := "# Introduction\nSome content"
	parser := NewResponseParser(content)

	_, err := parser.ExtractSection("Nonexistent")
	if err == nil {
		t.Error("Expected error when section not found")
	}
}

func TestResponseParser_RemoveMarkdown(t *testing.T) {
	content := `# Heading
**bold** and *italic*
[link](url)
` + "```code```" + `
inline ` + "`code`"

	parser := NewResponseParser(content)
	result := parser.RemoveMarkdown()

	if strings.Contains(result, "#") {
		t.Error("Expected headings to be removed")
	}
	if strings.Contains(result, "**") {
		t.Error("Expected bold markers to be removed")
	}
	if strings.Contains(result, "*") {
		t.Error("Expected italic markers to be removed")
	}
	if strings.Contains(result, "[") || strings.Contains(result, "]") {
		t.Error("Expected link markers to be removed")
	}
}

func TestResponseParser_GetPlainText(t *testing.T) {
	content := "**Bold** text"
	parser := NewResponseParser(content)

	result := parser.GetPlainText()

	if strings.Contains(result, "**") {
		t.Error("Expected markdown to be removed")
	}
}

func TestResponseParser_IsEmpty(t *testing.T) {
	parser := NewResponseParser("")
	if !parser.IsEmpty() {
		t.Error("Expected empty content to return true")
	}

	parser = NewResponseParser("   \n  \t  ")
	if !parser.IsEmpty() {
		t.Error("Expected whitespace-only content to return true")
	}

	parser = NewResponseParser("content")
	if parser.IsEmpty() {
		t.Error("Expected non-empty content to return false")
	}
}

func TestResponseParser_Length(t *testing.T) {
	content := "test content"
	parser := NewResponseParser(content)

	if parser.Length() != len(content) {
		t.Errorf("Expected length %d, got: %d", len(content), parser.Length())
	}
}
