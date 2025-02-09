package memcached

type codecValue interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}
