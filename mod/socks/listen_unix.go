//go:build !windows

package socks

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// // // // // // // // // //

// listenUnix opens a Unix socket with stale file handling.
func listenUnix(path string, mode os.FileMode) (net.Listener, error) {
	if err := validatePrivateSocketDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	ln, err := listenUnixSocket(path)
	if err == nil {
		return chmodUnixSocket(path, ln, mode)
	}
	if !isAddrInUse(err) {
		return nil, err
	}
	before, statErr := os.Lstat(path)
	if statErr != nil {
		return nil, fmt.Errorf("lstat unix socket %s: %w", path, statErr)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrSymlinkRefusal, path)
	}
	if before.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSocketRefusal, path)
	}
	// EADDRINUSE: check if the owning process is still alive.
	dialer := net.Dialer{Timeout: time.Second}
	probe, dialErr := dialer.Dial("unix", path)
	if dialErr == nil {
		_ = probe.Close()
		return nil, fmt.Errorf("%w on %q", ErrAlreadyListening, path)
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return nil, fmt.Errorf("probe unix socket %s: %w", path, dialErr)
	}
	// The process is gone; remove only the same verified socket inode observed
	// before the probe, so a replacement path cannot be unlinked by a TOCTOU race.
	if rmErr := removeUnixSocket(path, before); rmErr != nil {
		return nil, rmErr
	}
	ln, err = listenUnixSocket(path)
	if err != nil {
		return nil, err
	}
	return chmodUnixSocket(path, ln, mode)
}

// listenUnixSocket binds inside a validated private directory. That directory
// is the access-control boundary until chmod applies the final socket mode, so no
// process-global umask mutation is needed.
func listenUnixSocket(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if unixListener, ok := ln.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(true)
	}
	return ln, err
}

func validatePrivateSocketDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("unix socket directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s must be a private directory (0700 or stricter)", ErrUnsafeSocketDir, path)
	}
	return nil
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
func removeUnixSocket(path string, expected os.FileInfo) error {
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
	if expected == nil || !os.SameFile(expected, fi) {
		return fmt.Errorf("%w: %s changed during stale-socket probe", ErrSocketChanged, path)
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
