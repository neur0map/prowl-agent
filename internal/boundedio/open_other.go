//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package boundedio

import "os"

func openReadOnlyNonblocking(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
