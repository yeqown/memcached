package memcached

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_protocolBuilder(t *testing.T) {
	// test build a set command
	builder := newProtocolBuilder().
		AddString("set").AddString("key").AddInt(0).AddInt(0).AddInt(5).
		AddCRLF().
		AddString("value")
	expected := "set key 0 0 5\r\nvalue\r\n"
	assert.Equal(t, expected, string(builder.build()))
}
