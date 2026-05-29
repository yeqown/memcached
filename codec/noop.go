package codec

var (
	// Noop is the default codec that leaves values and flags unchanged.
	Noop = &NoopCodec{}
)

// NoopCodec passes key, value and flags through unchanged.
type NoopCodec struct{}

// Encode returns the value and flags unchanged.
func (*NoopCodec) Encode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

// Decode returns the value and flags unchanged.
func (*NoopCodec) Decode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

// SupportsOperation allows all operations.
func (*NoopCodec) SupportsOperation(string) error {
	return nil
}
