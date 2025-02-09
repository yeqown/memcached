package protocol

import "errors"

var (
	ErrInvalidResponse = errors.New("invalid response")
	ErrInvalidCommand  = errors.New("invalid command")
	ErrServerError     = errors.New("server error")
	ErrNotFound        = errors.New("key not found")
	ErrExists          = errors.New("key exists")
)
