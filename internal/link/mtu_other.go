//go:build !linux

package link

// linkMTUs reports nothing where there is no netlink to ask. The interface list
// still comes from the stdlib, and a zero MTU says it was not read rather than
// inventing a number.
func linkMTUs() map[string]int { return nil }
