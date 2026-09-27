package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
)

// TodoStore persists a session's todo list.
type TodoStore interface {
	ReplaceTodos(ctx context.Context, id core.SessionID, t []core.Todo) error
}

// todoInput is the JSON input todo accepts.
type todoInput struct {
	Todos []core.Todo `json:"todos"`
}

// todoTool implements ext.Tool for the "todo" tool.
type todoTool struct {
	store TodoStore
	pub   event.Publisher
}

// NewTodo returns the "todo" tool, backed by store, publishing
// event.TodosUpdated on pub after every successful update.
func NewTodo(store TodoStore, pub event.Publisher) ext.Tool {
	return &todoTool{store: store, pub: pub}
}

func (t *todoTool) Name() string { return "todo" }

func (t *todoTool) Description() string {
	return "Replace the current todo list with the full, updated list. " +
		"At most one item may be in_progress at a time."
}

func (t *todoTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"todos": map[string]any{
			"type":        "array",
			"description": "The full todo list, replacing the previous one.",
			"items": objectSchema(map[string]any{
				"content": stringProp("A short description of the task."),
				"status":  stringProp("One of pending, in_progress, completed."),
			}, "content", "status"),
		},
	}, "todos")
}

func (t *todoTool) Concurrent() bool { return false }

// Run implements ext.Tool.
func (t *todoTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in todoInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return errResult(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if errMsg := validateTodos(in.Todos); errMsg != "" {
		return errResult(call, errMsg), nil
	}

	if err := t.store.ReplaceTodos(ctx, rc.SessionID, in.Todos); err != nil {
		return errResult(call, err.Error()), nil
	}

	t.pub.Publish(event.TodosUpdated{Base: event.Base{SessionID: rc.SessionID}, Todos: in.Todos})

	return okResult(call, renderTodos(in.Todos)), nil
}

// validateTodos checks every todo's status is one of pending, in_progress,
// completed, that content is non-empty, and that at most one item is
// in_progress. It returns an error message, or "" if in is valid.
func validateTodos(in []core.Todo) string {
	inProgress := 0
	for _, td := range in {
		if strings.TrimSpace(td.Content) == "" {
			return "content is required for every todo"
		}
		switch td.Status {
		case "pending", "completed":
		case "in_progress":
			inProgress++
		default:
			return fmt.Sprintf("invalid status %q", td.Status)
		}
	}
	if inProgress > 1 {
		return "only one todo may be in_progress"
	}
	return ""
}

// renderTodos formats a short checklist: "[ ]" pending, "[~]" in_progress,
// "[x]" completed.
func renderTodos(todos []core.Todo) string {
	if len(todos) == 0 {
		return "(no todos)"
	}
	lines := make([]string, len(todos))
	for i, td := range todos {
		mark := " "
		switch td.Status {
		case "in_progress":
			mark = "~"
		case "completed":
			mark = "x"
		}
		lines[i] = fmt.Sprintf("[%s] %s", mark, td.Content)
	}
	return strings.Join(lines, "\n")
}
