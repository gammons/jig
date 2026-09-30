package core

import (
	"context"
	"log/slog"
	"slices"
)

type logAttrsKey struct{}

// WithLogAttrs returns a child context carrying the parent's log attributes
// with any same-key entry replaced by attrs. The parent's key order is kept
// and new keys are appended in call order; the parent is never modified.
func WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	merged := LogAttrs(ctx)
	for _, a := range attrs {
		i := slices.IndexFunc(merged, func(m slog.Attr) bool { return m.Key == a.Key })
		if i >= 0 {
			merged[i] = a
		} else {
			merged = append(merged, a)
		}
	}
	return context.WithValue(ctx, logAttrsKey{}, merged)
}

// LogAttrs returns a copy of ctx's log attributes, or nil if it has none.
func LogAttrs(ctx context.Context) []slog.Attr {
	attrs, _ := ctx.Value(logAttrsKey{}).([]slog.Attr)
	return slices.Clone(attrs)
}

// LogArgs returns ctx's log attributes as []any, for logger.With(...) and
// logger.Debug(msg, ...).
func LogArgs(ctx context.Context) []any {
	attrs := LogAttrs(ctx)
	if len(attrs) == 0 {
		return nil
	}
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return args
}
