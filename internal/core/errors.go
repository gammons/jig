package core

import "errors"

// ErrBusy is returned when a session already has a running turn.
var ErrBusy = errors.New("session is busy")
