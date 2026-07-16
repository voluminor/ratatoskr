package common

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTaskGroupStopCancelsAndWaits(t *testing.T) {
	group := NewTaskGroup(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	if !group.Go(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		<-release
	}) {
		t.Fatal("first task was rejected")
	}
	<-started
	done := group.Stop()
	select {
	case <-done:
		t.Fatal("Stop completed before the task exited")
	default:
	}
	if group.Go(func(context.Context) {}) {
		t.Fatal("task accepted after Stop")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete")
	}
	if done != group.Stop() {
		t.Fatal("Stop returned a different completion channel")
	}
}

func TestTaskGroupGoStopRace(t *testing.T) {
	for range 100 {
		group := NewTaskGroup(context.Background())
		start := make(chan struct{})
		var callers sync.WaitGroup
		for range 16 {
			callers.Add(1)
			go func() {
				defer callers.Done()
				<-start
				group.Go(func(context.Context) {})
			}()
		}
		close(start)
		done := group.Stop()
		callers.Wait()
		<-done
	}
}

func TestTaskGroupDoIsTracked(t *testing.T) {
	group := NewTaskGroup(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		_, ok := group.Do(func(context.Context) error {
			close(started)
			<-release
			return nil
		})
		if !ok {
			t.Error("Do was rejected before Stop")
		}
		close(finished)
	}()
	<-started
	done := group.Stop()
	select {
	case <-done:
		t.Fatal("Stop did not wait for Do")
	default:
	}
	close(release)
	<-finished
	<-done
	if _, ok := group.Do(func(context.Context) error { return nil }); ok {
		t.Fatal("Do was accepted after Stop")
	}
}
