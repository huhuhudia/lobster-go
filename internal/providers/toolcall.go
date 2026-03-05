package providers

import "encoding/json"

// ToolCallAdapter converts between internal tool calls and provider wire format.
type ToolCallAdapter interface {
	ToWire([]ToolCall) []map[string]interface{}
	Normalize([]ToolCall)
}

// OpenAIAdapter formats tool calls using OpenAI-compatible schema.
type OpenAIAdapter struct{}

func (OpenAIAdapter) ToWire(calls []ToolCall) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		args := call.Arguments
		if args == nil && call.Function != nil && call.Function.Arguments != "" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(call.Function.Arguments), &parsed); err == nil {
				args = parsed
			}
		}
		argBytes, _ := json.Marshal(args)
		if argBytes == nil {
			argBytes = []byte("{}")
		}
		out = append(out, map[string]interface{}{
			"id":   call.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      call.Name,
				"arguments": string(argBytes),
			},
		})
	}
	return out
}

func (OpenAIAdapter) Normalize(calls []ToolCall) {
	for i := range calls {
		if calls[i].Function != nil {
			if calls[i].Name == "" {
				calls[i].Name = calls[i].Function.Name
			}
			if calls[i].Arguments == nil && calls[i].Function.Arguments != "" {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(calls[i].Function.Arguments), &args); err == nil {
					calls[i].Arguments = args
				}
			}
		}
	}
}
