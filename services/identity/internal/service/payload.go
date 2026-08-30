package service

import "encoding/json"

// mustPayload serializes an event payload.
//
// It panics on failure by design: the payloads in this package are literal maps of strings,
// so a marshaling error would mean the standard library is broken, not that the input was
// bad. Turning that into an error return would add a branch no test could ever reach.
func mustPayload(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic("service: an event payload could not be serialized: " + err.Error())
	}

	return raw
}
