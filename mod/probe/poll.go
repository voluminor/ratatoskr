package probe

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"
)

// // // // // // // // // //

const (
	defaultPollInterval     = 200 * time.Millisecond
	defaultLookupRetryEvery = time.Second
	defaultHopsWaitTimeout  = 2 * time.Second
)

// // // // // // // // // //

// Trace returns the available spanning-tree and pathfinder routes to key.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	var cancel context.CancelFunc
	ctx, cancel = o.boundedContext(ctx)
	defer cancel()
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		return result, o.enrichPath(ctx, result.TreePath)
	}

	o.Lookup(key)

	if result != nil {
		var resultErr error
		if result.TreePath != nil && result.Hops == nil {
			result, resultErr = o.pollFull(ctx, key, result)
		}
		if result != nil && result.TreePath != nil {
			resultErr = errors.Join(resultErr, o.enrichPath(ctx, result.TreePath))
		}
		return result, resultErr
	}

	o.logger.Infof("[probe] lookup started for %x", key[:8])
	result, err := o.pollFull(ctx, key, nil)
	if result != nil && result.TreePath != nil {
		err = errors.Join(err, o.enrichPath(ctx, result.TreePath))
	}
	return result, err
}

// //

func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey, initial *TraceResultObj) (*TraceResultObj, error) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	lastLookup := time.Now()
	result := initial
	var hopsDeadline time.Time
	if result != nil && result.TreePath != nil && result.Hops == nil {
		hopsDeadline = time.Now().Add(defaultHopsWaitTimeout)
	}

	for {
		select {
		case <-ctx.Done():
			if latest := o.collect(key); latest != nil {
				result = latest
			}
			if result != nil {
				return result, fmt.Errorf("%w: %w", ErrLookupTimedOut, ctx.Err())
			}
			return nil, fmt.Errorf("%w for key %x: %w", ErrLookupTimedOut, key[:8], ctx.Err())
		case <-ticker.C:
			now := time.Now()
			if latest := o.collect(key); latest != nil {
				result = latest
			}

			if result != nil && result.TreePath != nil && result.Hops != nil {
				return result, nil
			}

			if result != nil && result.TreePath != nil && result.Hops == nil {
				if hopsDeadline.IsZero() {
					hopsDeadline = now.Add(defaultHopsWaitTimeout)
					o.Lookup(key)
					lastLookup = now
				}
				if !now.Before(hopsDeadline) {
					return result, nil
				}
			}

			if now.Sub(lastLookup) >= o.lookupRetryEvery {
				o.Lookup(key)
				lastLookup = now
			}
		}
	}
}

// //

func (o *Obj) enrichPath(ctx context.Context, path []*NodeObj) error {
	if len(path) <= 1 {
		return nil
	}

	peerLatency := make(map[[ed25519.PublicKeySize]byte]time.Duration)
	for _, p := range o.source.GetPeers() {
		if p.Up && len(p.Key) == ed25519.PublicKeySize && p.Latency > 0 {
			peerLatency[toKeyArray(p.Key)] = p.Latency
		}
	}

	targets := path[1:]
	type rttResultObj struct {
		idx int
		rtt time.Duration
		err error
	}

	var remote []int
	for i, n := range targets {
		if lat, ok := peerLatency[toKeyArray(n.Key)]; ok {
			targets[i].RTT = lat
			continue
		}
		if n.RTT == 0 {
			remote = append(remote, i)
		}
	}
	if len(remote) == 0 {
		return nil
	}

	ch := make(chan rttResultObj, len(remote))
	jobs := make(chan int, len(remote))
	for _, idx := range remote {
		jobs <- idx
	}
	close(jobs)

	workerCount := len(remote)
	if workerCount > DefaultMaxConcurrency {
		workerCount = DefaultMaxConcurrency
	}
	for range workerCount {
		go func() {
			for idx := range jobs {
				_, rtt, err := o.callRemotePeers(ctx, targets[idx].Key)
				ch <- rttResultObj{idx: idx, rtt: rtt, err: err}
			}
		}()
	}
	var busy, closed bool
	for range remote {
		select {
		case r := <-ch:
			targets[r.idx].RTT = r.rtt
			busy = busy || errors.Is(r.err, ErrProbeBusy)
			closed = closed || errors.Is(r.err, ErrClosed)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if closed {
		return ErrClosed
	}
	if busy {
		return ErrProbeBusy
	}
	return nil
}

func (o *Obj) collect(key ed25519.PublicKey) *TraceResultObj {
	var result TraceResultObj
	if path, err := o.Path(key); err == nil {
		result.TreePath = path
	}
	if hops, err := o.Hops(key); err == nil {
		result.Hops = hops
	}
	if result.TreePath != nil || result.Hops != nil {
		return &result
	}
	return nil
}
