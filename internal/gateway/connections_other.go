//go:build !linux

package gateway

import "errors"

// readConntrack has no meaning off Linux: the connection table is a Linux
// interface. The endpoint reports that rather than an empty census, because
// "this build cannot see connections" and "nothing is connected" are different
// answers and only one of them is true.
func readConntrack() (conntrackSample, error) {
	return conntrackSample{}, errors.New("the connection census requires Linux")
}
