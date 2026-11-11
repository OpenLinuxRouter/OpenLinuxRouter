//go:build !linux

package clients

import (
	"context"
	"errors"
)

// Neighbours has no neighbour table to read off Linux, and says so once
// rather than reporting that nothing was heard.
type Neighbours struct{}

// Heard always fails off Linux.
func (Neighbours) Heard(context.Context) ([]string, error) {
	return nil, errors.New("the neighbour table is only readable on Linux")
}
