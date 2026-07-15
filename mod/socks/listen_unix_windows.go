//go:build windows

package socks

import (
	"fmt"
	"net"
	"os"
)

// // // // // // // // // //

func listenUnix(path string, _ os.FileMode) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix socket %s: %w", path, err)
	}
	return ln, nil
}

func removeUnixSocket(path string, _ os.FileInfo) error {
	return fmt.Errorf("%w: %s", ErrSocketRefusal, path)
}

func isAddrInUse(error) bool {
	return false
}
