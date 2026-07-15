package mobile

import (
	"sync"
	"testing"
	"time"
)

// // // // // // // // // //

func TestRatatoskrStartStopRestart(t *testing.T) {
	y := NewRatatoskr()
	for cycle := 0; cycle < 2; cycle++ {
		if err := y.Start("", ""); err != nil {
			t.Fatalf("Start cycle %d: %v", cycle, err)
		}
		if !y.IsRunning() {
			t.Fatalf("cycle %d did not report running", cycle)
		}
		done := make(chan error, 1)
		go func() { done <- y.Stop() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Stop cycle %d: %v", cycle, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Stop cycle %d deadlocked", cycle)
		}
	}
}

func TestRatatoskrConcurrentStop(t *testing.T) {
	y := NewRatatoskr()
	if err := y.Start("", ""); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 8)
	var callers sync.WaitGroup
	for range cap(errs) {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			errs <- y.Stop()
		}()
	}
	close(start)
	callers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Stop: %v", err)
		}
	}
}

type testLogCallbackObj struct {
	mu    sync.Mutex
	lines []string
}

func (c *testLogCallbackObj) Log(line string) {
	c.mu.Lock()
	c.lines = append(c.lines, line)
	c.mu.Unlock()
}

func TestLogBridgeDeliversSynchronously(t *testing.T) {
	bridge := newLogBridge()
	callback := &testLogCallbackObj{}
	bridge.setCallback(callback)
	bridge.Infof("value=%d", 7)
	callback.mu.Lock()
	defer callback.mu.Unlock()
	if len(callback.lines) != 1 || callback.lines[0] != "[INFO]  value=7" {
		t.Fatalf("callback lines = %q", callback.lines)
	}
}
