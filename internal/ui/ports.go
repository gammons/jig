package ui

import (
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// Ports bundles every port the App needs to drive jig's TUI. It holds no
// concrete client/data types (AGENTS.md: ui does no I/O of its own): every
// field is a core interface, or, for the event bus, a func the App calls
// to obtain a subscription, so ui never imports event.Bus itself.
type Ports struct {
	Chat     core.ChatService
	Sessions core.SessionService
	Perms    core.PermissionService
	Catalog  core.CatalogService
	Agents   core.AgentService
	Project  core.ProjectService
	Blobs    core.BlobService
	Prefs    core.PrefsService
	Editor   core.EditorService
	// Subscribe returns a fresh event.Subscription; the App calls it once
	// at startup.
	Subscribe func() *event.Subscription
}
