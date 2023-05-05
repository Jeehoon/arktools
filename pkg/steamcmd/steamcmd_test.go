package steamcmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUe4String(t *testing.T) {
	var buf = new(bytes.Buffer)

	assert.Nil(t, writeUe4String(buf, "hello!!!"))

	s, nread, err := readUe4String(buf)
	assert.Nil(t, err)
	assert.Equal(t, s, "hello!!!")
	assert.Equal(t, nread, 12)
}
