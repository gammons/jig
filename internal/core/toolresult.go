package core

// ToolError builds an IsError ToolResult for call with msg as its output.
// Every built-in tool package (service/tools, service/task,
// service/skills, ...) uses this instead of building a ToolResult by
// hand, so every tool's error output has the same shape.
func ToolError(call ToolCall, msg string) ToolResult {
	return ToolResult{CallID: call.ID, Name: call.Name, Output: msg, IsError: true}
}

// ToolOK builds a successful ToolResult for call with output as its
// output.
func ToolOK(call ToolCall, output string) ToolResult {
	return ToolResult{CallID: call.ID, Name: call.Name, Output: output}
}
