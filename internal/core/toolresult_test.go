package core

import (
	"reflect"
	"testing"
)

func TestToolError(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "read"}
	got := ToolError(call, "boom")
	want := ToolResult{CallID: "call_1", Name: "read", Output: "boom", IsError: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToolError = %+v, want %+v", got, want)
	}
}

func TestToolOK(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "read"}
	got := ToolOK(call, "contents")
	want := ToolResult{CallID: "call_1", Name: "read", Output: "contents"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToolOK = %+v, want %+v", got, want)
	}
}
