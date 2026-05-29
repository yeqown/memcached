package codec

var (
	Noop = &NoopCodec{}
)

// NoopCodec passes key, value and flags through unchanged.
type NoopCodec struct{}

func (*NoopCodec) Encode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (*NoopCodec) Decode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (*NoopCodec) SupportsOperation(string) error {
	return nil
}
