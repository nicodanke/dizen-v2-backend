package config

import "strconv"

// addr builds a listen address bound to every interface. Services always run inside a
// container, where binding to a single interface buys nothing and breaks health probes.
func addr(port int) string {
	return ":" + strconv.Itoa(port)
}
