package blitzortungc

import (
	"errors"
)

type UnmarshalError struct {
	Wrapped error
	RawData []byte
}

func (e UnmarshalError) Error() string        { return e.Wrapped.Error() }
func (e UnmarshalError) Unwrap() error        { return e.Wrapped }
func (e UnmarshalError) Is(target error) bool { return errors.Is(e.Wrapped, target) }

// InflateError is reported via the Client's ErrorHook when an incoming
// WebSocket message can't be inflated (typically because it contains
// invalid UTF-8). RawData carries the original compressed payload.
type InflateError struct {
	Wrapped error
	RawData []byte
}

func (e InflateError) Error() string        { return e.Wrapped.Error() }
func (e InflateError) Unwrap() error        { return e.Wrapped }
func (e InflateError) Is(target error) bool { return errors.Is(e.Wrapped, target) }
