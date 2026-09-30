package core

import "time"

// Session is a conversation thread. ParentID is set when the Session is a
// subagent's session, spawned from another Session.
type Session struct {
	ID        SessionID
	ParentID  SessionID
	Title     string
	Agent     string
	Model     string
	Effort    Effort // the session's chosen effort; "" = none chosen
	Cwd       string
	CreatedAt time.Time
	UpdatedAt time.Time
}
