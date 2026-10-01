package opencode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/pathid"
)

// Stats tallies what a translator (and, later, the reader that drives it)
// skipped, dropped, or could not translate while turning one opencode
// session into jig's core values.
type Stats struct {
	SkippedMessages       []string // "<msg id>: <err>"
	DroppedTypes          map[string]int
	UntranslatedTools     map[string]int
	UnimportedAttachments int
	EmptyMessages         int
}

// sessionRow is one session_v2 row, as the reader (Task 4) will load it.
type sessionRow struct {
	ID        string
	ParentID  string
	Directory string
	Title     string
	Agent     string
	ModelJSON string
	Created   int64
	Updated   int64
}

// messageRow is one session_message row, as the reader (Task 4) will load
// it, ordered by seq.
type messageRow struct {
	ID      string
	Type    string
	Data    string
	Created int64
}

// translator accumulates one session's translated core.Session and
// core.Message slice as messageRows are fed to it via add, and its Stats.
type translator struct {
	agents  map[string]bool
	sess    core.Session
	msgs    []core.Message
	stats   Stats
	lastDir string // location.directory from the session's last location-switched message
}

// sessionModel is the shape of session_v2.model.
type sessionModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}

// newTranslator builds a translator for row, ready to receive messageRows
// via add in seq order. agents is the set of agent names jig knows
// (built-ins plus configured), used to normalize the session's Agent.
func newTranslator(row sessionRow, agents map[string]bool) *translator {
	t := &translator{
		agents:  agents,
		lastDir: row.Directory,
		stats: Stats{
			DroppedTypes:      map[string]int{},
			UntranslatedTools: map[string]int{},
		},
	}

	title := row.Title
	if title == "" {
		title = "(untitled)"
	}

	var model sessionModel
	_ = json.Unmarshal([]byte(row.ModelJSON), &model)

	t.sess = core.Session{
		ID:        core.SessionID(row.ID),
		ParentID:  core.SessionID(row.ParentID),
		Title:     title,
		Agent:     normalizeAgent(agents, row.ParentID != "", row.Agent),
		Model:     modelString(model.ProviderID, model.ID),
		Effort:    parseVariant(model.Variant),
		Cwd:       pathid.Key(row.Directory),
		CreatedAt: time.UnixMilli(row.Created),
		UpdatedAt: time.UnixMilli(row.Updated),
	}
	return t
}

// modelString builds jig's Model string from a providerID and id, "" if
// either is absent.
func modelString(providerID, id string) string {
	if providerID == "" || id == "" {
		return ""
	}
	return providerID + "/" + id
}

// parseVariant converts an opencode variant into a jig Effort: accepted
// and not "default" → itself; otherwise "" (including a rejected or
// "default" variant).
func parseVariant(variant string) core.Effort {
	e, err := core.ParseEffort(variant)
	if err != nil {
		return ""
	}
	if variant == "default" {
		return ""
	}
	return e
}

// normalizeAgent applies spec §5.1/§5.3's agent normalization, shared by
// the session's own Agent and every assistant message's Agent: keep agent
// if it's in agents, else "build" for a root session, "general" for a
// child.
func normalizeAgent(agents map[string]bool, isChild bool, agent string) string {
	if agents[agent] {
		return agent
	}
	if isChild {
		return "general"
	}
	return "build"
}

// add translates one messageRow and appends the resulting message(s) (zero
// or one, except a synthetic message merging into an existing one adds
// none) to t.msgs, updating t.stats as it goes.
func (t *translator) add(m messageRow) {
	switch m.Type {
	case "user":
		t.addUser(m)
	case "synthetic":
		t.addSynthetic(m)
	case "assistant":
		t.addAssistant(m)
	case "compaction":
		t.addCompaction(m)
	case "location-switched":
		t.addLocationSwitched(m)
	default:
		t.stats.DroppedTypes[m.Type]++
	}
}

// finish returns the translated session, its messages in order, and the
// accumulated Stats.
func (t *translator) finish() (core.Session, []core.Message, Stats) {
	return t.sess, t.msgs, t.stats
}

// skip records a bad-JSON message in t.stats.
func (t *translator) skip(m messageRow, err error) {
	t.stats.SkippedMessages = append(t.stats.SkippedMessages, fmt.Sprintf("%s: %v", m.ID, err))
}

// userData is the shape of a "user" message's data.
type userData struct {
	Text  string     `json:"text"`
	Files []userFile `json:"files"`
}

// userFile is one entry of a user message's files[].
type userFile struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"` // base64
}

// addUser translates a "user" message into a RoleUser core.Message, per
// spec §5.2.
func (t *translator) addUser(m messageRow) {
	var data userData
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.skip(m, err)
		return
	}

	var parts []core.Part
	if data.Text != "" {
		parts = append(parts, core.Part{Kind: core.PartText, Text: data.Text})
	}
	for _, f := range data.Files {
		parts = append(parts, t.translateUserFile(f))
	}

	if len(parts) == 0 {
		t.stats.EmptyMessages++
		return
	}

	t.msgs = append(t.msgs, core.Message{
		ID:        core.MessageID(m.ID),
		SessionID: t.sess.ID,
		Role:      core.RoleUser,
		Agent:     t.sess.Agent,
		Model:     t.sess.Model,
		Parts:     parts,
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(m.Created),
	})
}

// translateUserFile converts one user file into the Part it becomes: an
// image MIME → PartAttachment with decoded Media; a text/* MIME →
// PartAttachment with Content; anything else → PartText noting it was not
// imported (and counted).
func (t *translator) translateUserFile(f userFile) core.Part {
	decoded, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		t.stats.UnimportedAttachments++
		return core.Part{Kind: core.PartText, Text: fmt.Sprintf("[attachment not imported: %s (%s)]", f.Name, f.MIME)}
	}

	switch {
	case strings.HasPrefix(f.MIME, "image/"):
		return core.Part{Kind: core.PartAttachment, Attachment: &core.Attachment{
			Path:  f.Name,
			Media: &core.Media{MIME: f.MIME, Data: decoded},
		}}
	case strings.HasPrefix(f.MIME, "text/"):
		return core.Part{Kind: core.PartAttachment, Attachment: &core.Attachment{
			Path:    f.Name,
			Content: string(decoded),
		}}
	default:
		t.stats.UnimportedAttachments++
		return core.Part{Kind: core.PartText, Text: fmt.Sprintf("[attachment not imported: %s (%s)]", f.Name, f.MIME)}
	}
}

// syntheticData is the shape of a "synthetic" message's data.
type syntheticData struct {
	Text string `json:"text"`
}

// addSynthetic translates a "synthetic" message: its text is appended as a
// PartText to the previous message if that message is a user message;
// otherwise it becomes its own user message.
func (t *translator) addSynthetic(m messageRow) {
	var data syntheticData
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.skip(m, err)
		return
	}

	if n := len(t.msgs); n > 0 && t.msgs[n-1].Role == core.RoleUser {
		t.msgs[n-1].Parts = append(t.msgs[n-1].Parts, core.Part{Kind: core.PartText, Text: data.Text})
		return
	}

	t.msgs = append(t.msgs, core.Message{
		ID:        core.MessageID(m.ID),
		SessionID: t.sess.ID,
		Role:      core.RoleUser,
		Agent:     t.sess.Agent,
		Model:     t.sess.Model,
		Parts:     []core.Part{{Kind: core.PartText, Text: data.Text}},
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(m.Created),
	})
}

// compactionData is the shape of a "compaction" message's data.
type compactionData struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// addCompaction translates a "compaction" message with status "completed"
// into a RoleAssistant message with exactly one PartCompaction part;
// other statuses are dropped and counted.
func (t *translator) addCompaction(m messageRow) {
	var data compactionData
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.skip(m, err)
		return
	}
	if data.Status != "completed" {
		t.stats.DroppedTypes["compaction"]++
		return
	}

	t.msgs = append(t.msgs, core.Message{
		ID:        core.MessageID(m.ID),
		SessionID: t.sess.ID,
		Role:      core.RoleAssistant,
		Agent:     t.sess.Agent,
		Model:     t.sess.Model,
		Parts:     []core.Part{{Kind: core.PartCompaction, Text: data.Summary}},
		Status:    core.StatusComplete,
		CreatedAt: time.UnixMilli(m.Created),
	})
}

// locationSwitchedData is the shape of a "location-switched" message's
// data.
type locationSwitchedData struct {
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

// addLocationSwitched records the message's location.directory as the
// session's Cwd candidate (the last one wins) and counts it as dropped: it
// produces no message of its own.
func (t *translator) addLocationSwitched(m messageRow) {
	var data locationSwitchedData
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.skip(m, err)
		return
	}
	t.stats.DroppedTypes["location-switched"]++
	if data.Location.Directory == "" {
		return
	}
	t.lastDir = data.Location.Directory
	t.sess.Cwd = pathid.Key(t.lastDir)
}
