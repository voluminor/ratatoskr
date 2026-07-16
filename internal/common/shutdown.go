package common

import (
	"errors"
	"fmt"
	"time"
)

// NamedCloseObj associates a shutdown function with its error prefix.
type NamedCloseObj struct {
	// Name prefixes an error returned by Close.
	Name string
	// Close performs the shutdown operation. A nil function succeeds.
	Close func() error
}

type closeResultObj struct {
	name string
	err  error
}

func executeClose(closeObj NamedCloseObj) closeResultObj {
	var err error
	if closeObj.Close != nil {
		err = closeObj.Close()
	}
	return closeResultObj{name: closeObj.Name, err: err}
}

func startClose(closeObj NamedCloseObj) <-chan closeResultObj {
	result := make(chan closeResultObj, 1)
	go func() { result <- executeClose(closeObj) }()
	return result
}

func appendCloseError(errs []error, result closeResultObj) []error {
	if result.err == nil {
		return errs
	}
	if result.name == "" {
		return append(errs, result.err)
	}
	return append(errs, fmt.Errorf("%s: %w", result.name, result.err))
}

// CloseWithDeadline closes before concurrently and then starts final. The
// shared timeout covers both phases. Timed-out operations continue in owned
// goroutines, while the function returns errors observed before the deadline.
func CloseWithDeadline(timeout time.Duration, before []NamedCloseObj, final NamedCloseObj) (err error, timedOut bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	errs := make([]error, 0, len(before)+1)
	results := make(chan closeResultObj, len(before))
	for _, closeObj := range before {
		go func() { results <- executeClose(closeObj) }()
	}
	for range before {
		select {
		case result := <-results:
			errs = appendCloseError(errs, result)
		case <-timer.C:
			_ = startClose(final)
			return errors.Join(errs...), true
		}
	}
	select {
	case result := <-startClose(final):
		errs = appendCloseError(errs, result)
		return errors.Join(errs...), false
	case <-timer.C:
		return errors.Join(errs...), true
	}
}
