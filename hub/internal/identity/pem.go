package identity

import (
	"bytes"
	"encoding/pem"
)

func pemEncode(typ string, body []byte) []byte {
	var buf bytes.Buffer
	_ = pem.Encode(&buf, &pem.Block{Type: typ, Bytes: body})
	return buf.Bytes()
}
