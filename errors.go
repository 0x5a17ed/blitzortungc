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
