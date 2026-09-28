package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
)

// readFileCmd returns a Cmd that reads path through p.Project and hands
// the result to done, on ui's behalf: the only I/O here is the port call,
// never a direct file read.
func readFileCmd(p Ports, path string, done func([]byte, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		data, err := p.Project.ReadFile(context.Background(), path)
		return done(data, err)
	}
}

// openBlobCmd returns a Cmd that opens ref through p.Blobs and hands the
// result to done.
func openBlobCmd(p Ports, ref string, done func([]byte, string, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		data, mime, err := p.Blobs.Open(ref)
		return done(data, mime, err)
	}
}

// sessionMessagesCmd returns a Cmd that lists id's messages through
// p.Sessions and hands the result to done.
func sessionMessagesCmd(p Ports, id core.SessionID, done func([]core.Message, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msgs, err := p.Sessions.Messages(context.Background(), id)
		return done(msgs, err)
	}
}
