package app

import (
	"context"
	"fmt"
	"time"
)

// sessionsListLimit caps how many sessions `jig sessions` lists.
const sessionsListLimit = 100

// sessionsCmd implements `jig sessions`: root sessions, newest first, as
// "<id>  <updated RFC3339>  <title>".
func sessionsCmd(ctx context.Context, args []string, std Stdio, getenv func(string) string) int {
	if len(args) != 0 {
		fmt.Fprintln(std.Err, "usage: jig sessions")
		return exitConfig
	}
	e, err := loadEnv("", getenv)
	if err != nil {
		fmt.Fprintln(std.Err, err)
		return exitConfig
	}
	st, err := openStore(ctx, e)
	if err != nil {
		fmt.Fprintln(std.Err, "error:", err)
		return exitRunFailed
	}
	defer st.Close()
	list, err := st.ListSessions(ctx, "", sessionsListLimit)
	if err != nil {
		fmt.Fprintln(std.Err, "error:", err)
		return exitRunFailed
	}
	for _, s := range list {
		fmt.Fprintf(std.Out, "%s  %s  %s\n", s.ID, s.UpdatedAt.Format(time.RFC3339), s.Title)
	}
	return exitOK
}
