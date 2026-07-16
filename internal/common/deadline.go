package common

import (
	"sync"
	"sync/atomic"
	"time"
)

// // // // // // // // // //

type deadlineAction uint8

const (
	deadlineSkip deadlineAction = iota
	deadlineArm
	deadlineClear
)

func deadlineRefresh(now time.Time, timeout time.Duration, currentNanos int64) (deadlineAction, time.Time) {
	if timeout <= 0 {
		return deadlineClear, time.Time{}
	}
	refreshAfter := timeout / 2
	if refreshAfter <= 0 {
		refreshAfter = timeout
	}
	refreshAt := now.Add(refreshAfter).UnixNano()
	if currentNanos > refreshAt {
		return deadlineSkip, time.Time{}
	}
	return deadlineArm, now.Add(timeout)
}

// DeadlineConnInterface is the deadline surface used by RefreshDeadline.
type DeadlineConnInterface interface {
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
}

// DeadlineGateObj serializes deadline changes and keeps the last successful
// deadline in atomic state for the common no-refresh path.
type DeadlineGateObj struct {
	mu    sync.Mutex
	state atomic.Int64
}

// RefreshDeadline refreshes an idle deadline after half its duration has
// elapsed. Non-positive timeouts clear an armed deadline. Read-only mode uses
// SetReadDeadline; other calls use SetDeadline.
func RefreshDeadline(now time.Time, timeout time.Duration, gate *DeadlineGateObj, conn DeadlineConnInterface, readOnly bool) {
	if timeout <= 0 && gate.state.Load() == 0 {
		return
	}
	action, _ := deadlineRefresh(now, timeout, gate.state.Load())
	if action == deadlineSkip {
		return
	}

	gate.mu.Lock()
	defer gate.mu.Unlock()
	action, deadline := deadlineRefresh(now, timeout, gate.state.Load())
	switch action {
	case deadlineArm:
		var err error
		if readOnly {
			err = conn.SetReadDeadline(deadline)
		} else {
			err = conn.SetDeadline(deadline)
		}
		if err == nil {
			gate.state.Store(deadline.UnixNano())
		}
	case deadlineClear:
		if gate.state.Load() != 0 {
			var err error
			if readOnly {
				err = conn.SetReadDeadline(time.Time{})
			} else {
				err = conn.SetDeadline(time.Time{})
			}
			if err == nil {
				gate.state.Store(0)
			}
		}
	}
}
