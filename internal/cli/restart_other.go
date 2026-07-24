//go:build !linux

package cli

// Process discovery currently relies on Linux /proc. Restart still rebuilds the
// index on other platforms; clients reconnect when their current process exits.
func findProwlServers(string) []int { return nil }

func stopServers([]int) int { return 0 }
