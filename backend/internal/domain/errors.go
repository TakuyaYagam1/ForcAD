package domain

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrConflict           = errors.New("data conflicts with existing state")
	ErrEventCursorInvalid = errors.New("event cursor is invalid")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type StateError struct{ Message string }

func (e *StateError) Error() string { return e.Message }
