package core

// AgentBrowserCache remembers the result of the last agent-browser skill
// discovery, keyed by the binary's mtime, so jig can skip re-running the
// (up to 3 s) discovery when the binary hasn't changed.
type AgentBrowserCache struct {
	Bin       string
	ModTime   int64
	SkillsDir string
}

// Prefs is jig's persisted per-user UI preferences: state that belongs to
// neither a session nor a project's config. See prefsfs for its on-disk
// form.
type Prefs struct {
	Theme        string
	Sidebar      *bool               // nil = automatic (by width)
	Recent       []string            // action IDs, most recent first, ≤ 5
	History      map[string][]string // project path → prompts, newest last, ≤ 100
	AgentBrowser AgentBrowserCache
}

// PrefsService is the port UIs use to read and persist Prefs. Update
// applies fn to the latest persisted Prefs (re-read, not a cached copy)
// and saves the result, so concurrent writers of different fields never
// lose each other's changes.
type PrefsService interface {
	Get() Prefs
	Update(fn func(*Prefs)) error
}
