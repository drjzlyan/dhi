//go:build !unix

package hosttools

// writable is unknown here; npm -g is not offered.
func writable(string) bool { return false }
