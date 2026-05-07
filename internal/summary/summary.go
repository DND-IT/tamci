package summary

import (
	"encoding/json"
	"fmt"
)

// DeserializeNestedJSON recursively walks parsed JSON, attempting json.Unmarshal
// on string values that may contain serialized JSON.
func DeserializeNestedJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = DeserializeNestedJSON(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = DeserializeNestedJSON(v)
		}
		return result
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(val), &parsed); err == nil {
			return DeserializeNestedJSON(parsed)
		}
		return val
	default:
		return val
	}
}

// FormatOutput generates the markdown output with a header, details block, and code block.
func FormatOutput(header, dataType, content string) string {
	return fmt.Sprintf("## %s\n<details><summary>Click to expand</summary>\n\n```%s\n%s\n```\n</details>\n", header, dataType, content)
}
