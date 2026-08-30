package jwt

import (
	"bytes"
	"encoding/pem"
)

// nextPublicKey reads the first PEM block of data, returning the key and what is left.
// A nil key with no error means there are no more blocks.
func nextPublicKey(data []byte) (*Key, []byte, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil, nil
	}

	block, rest := pem.Decode(data)
	if block == nil {
		return nil, nil, nil
	}

	// The block is re-encoded so the parser receives exactly one, which is what keeps the
	// concatenated form working.
	key, err := ParsePublicKeyPEM(pem.EncodeToMemory(block))
	if err != nil {
		return nil, nil, err
	}

	return &key, rest, nil
}
