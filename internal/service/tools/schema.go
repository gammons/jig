package tools

// objectSchema builds a JSON Schema object with the given properties and
// required field names.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// stringProp builds a JSON Schema string property with description desc.
func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// intProp builds a JSON Schema integer property with description desc.
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// boolProp builds a JSON Schema boolean property with description desc.
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
