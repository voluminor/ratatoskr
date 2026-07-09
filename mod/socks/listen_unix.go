//go:build !windows

package socks

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// // // // // // // // // //

// listenUnix opens a Unix socket with stale file handling.
func listenUnix(path string, mode os.FileMode) (net.Listener, error) {
	ln, err := listenUnixWithMode(path)
	if err == nil {
		return chmodUnixSocket(path, ln, mode)
	}
	if !isAddrInUse(err) {
		return nil, err
	}
	// EADDRINUSE: check if the owning process is still alive.
	dialer := net.Dialer{Timeout: time.Second}
	probe, dialErr := dialer.Dial("unix", path)
	if dialErr == nil {
		_ = probe.Close()
		return nil, fmt.Errorf("%w on %q", ErrAlreadyListening, path)
	}
	// The process is gone; remove only verified socket files.
	if rmErr := removeUnixSocket(path); rmErr != nil {
		return nil, rmErr
	}
	ln, err = listenUnixWithMode(path)
	if err != nil {
		return nil, err
	}
	return chmodUnixSocket(path, ln, mode)
}

func listenUnixWithMode(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

func chmodUnixSocket(path string, ln net.Listener, mode os.FileMode) (net.Listener, error) {
	if err := os.Chmod(path, mode.Perm()); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chmod unix socket %s: %w", path, err)
	}
	return ln, nil
}

// removeUnixSocket removes only stale Unix socket filesystem entries.
func removeUnixSocket(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("os.Lstat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlinkRefusal, path)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%w: %s", ErrSocketRefusal, path)
	}
	return os.Remove(path)
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.EADDRINUSE)
		}
	}
	return false
}
