package summary

import (
	"encoding/json"
	"testing"
)

func TestDeserializeNestedJSON_PlainString(t *testing.T) {
	if got := DeserializeNestedJSON("hello world"); got != "hello world" {
		t.Errorf("expected 'hello world', got %v", got)
	}
}

func TestDeserializeNestedJSON_Number(t *testing.T) {
	if got := DeserializeNestedJSON(42.0); got != 42.0 {
		t.Errorf("expected 42.0, got %v", got)
	}
}

func TestDeserializeNestedJSON_Bool(t *testing.T) {
	if got := DeserializeNestedJSON(true); got != true {
		t.Errorf("expected true, got %v", got)
	}
}

func TestDeserializeNestedJSON_Nil(t *testing.T) {
	if got := DeserializeNestedJSON(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestDeserializeNestedJSON_NestedJSONString(t *testing.T) {
	input := map[string]any{"key": `{"nested": "value"}`}
	m := DeserializeNestedJSON(input).(map[string]any)
	nested, ok := m["key"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", m["key"])
	}
	if nested["nested"] != "value" {
		t.Errorf("expected 'value', got %v", nested["nested"])
	}
}

func TestDeserializeNestedJSON_DeeplyNested(t *testing.T) {
	innerJSON := `{"deep": "data"}`
	outer, _ := json.Marshal(innerJSON)
	input := map[string]any{"key": string(outer)}
	m := DeserializeNestedJSON(input).(map[string]any)
	nested, ok := m["key"].(map[string]any)
	if !ok {
		t.Fatalf("expected deeply nested map, got %T", m["key"])
	}
	if nested["deep"] != "data" {
		t.Errorf("expected 'data', got %v", nested["deep"])
	}
}

func TestDeserializeNestedJSON_Array(t *testing.T) {
	input := []any{"plain", `{"a": 1}`}
	arr := DeserializeNestedJSON(input).([]any)
	if arr[0] != "plain" {
		t.Errorf("expected 'plain', got %v", arr[0])
	}
	nested, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", arr[1])
	}
	if nested["a"] != 1.0 {
		t.Errorf("expected 1, got %v", nested["a"])
	}
}

func TestDeserializeNestedJSON_Dict(t *testing.T) {
	input := map[string]any{
		"plain": "text",
		"num":   42.0,
		"obj":   `{"inner": true}`,
	}
	m := DeserializeNestedJSON(input).(map[string]any)
	if m["plain"] != "text" {
		t.Errorf("expected 'text', got %v", m["plain"])
	}
	if m["num"] != 42.0 {
		t.Errorf("expected 42.0, got %v", m["num"])
	}
	inner := m["obj"].(map[string]any)
	if inner["inner"] != true {
		t.Errorf("expected true, got %v", inner["inner"])
	}
}

func TestFormatOutput_Basic(t *testing.T) {
	got := FormatOutput("Test Header", "json", `{"key": "value"}`)
	want := "## Test Header\n<details><summary>Click to expand</summary>\n\n```json\n{\"key\": \"value\"}\n```\n</details>\n"
	if got != want {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestFormatOutput_EmptyDataType(t *testing.T) {
	got := FormatOutput("Summary", "", "plain text")
	want := "## Summary\n<details><summary>Click to expand</summary>\n\n```\nplain text\n```\n</details>\n"
	if got != want {
		t.Errorf("unexpected output:\n%s", got)
	}
}
