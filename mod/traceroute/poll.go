package traceroute

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"
)

// // // // // // // // // //

const (
	pollInterval     = 200 * time.Millisecond
	hopsGracePeriod  = 10 // ticks to wait for hops after tree is found
	lookupRetryEvery = time.Second
)

// // // // // // // // // //

// Trace searches for the key in both spanning tree and pathfinder.
// Strategy: both available → return immediately; tree only → Lookup + wait 2s for hops;
// nothing → full poll with lookup retries every second until ctx expires.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		return result, nil
	}

	o.Lookup(key)

	if result != nil {
		if result.Hops == nil {
			enriched := o.pollHops(ctx, key, 2*time.Second)
			if enriched != nil {
				result.Hops = enriched
			}
		}
		return result, nil
	}

	o.logger.Infof("[traceroute] lookup started for %x", key[:8])
	return o.pollFull(ctx, key)
}

// //

// pollHops waits for hops to appear within maxWait. One retry lookup after ~1s.
func (o *Obj) pollHops(ctx context.Context, key ed25519.PublicKey, maxWait time.Duration) []HopObj {
	startTime := time.Now()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	retried := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if time.Since(startTime) > maxWait {
				hops, _ := o.Hops(key)
				return hops
			}
			if hops, err := o.Hops(key); err == nil {
				return hops
			}
			if !retried && time.Since(startTime) > time.Second {
				o.Lookup(key)
				retried = true
			}
		}
	}
}

// //

// pollFull polls for both tree path and hops until ctx expires.
// Single flat loop: once tree is found, gives hopsGracePeriod extra ticks for hops.
func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	lastLookup := time.Now()
	graceTicks := -1

	for {
		select {
		case <-ctx.Done():
			if result := o.collect(key); result != nil {
				return result, fmt.Errorf("%w: %w", ErrLookupTimedOut, ctx.Err())
			}
			return nil, fmt.Errorf("%w for key %x: %w", ErrLookupTimedOut, key[:8], ctx.Err())
		case <-ticker.C:
			result := o.collect(key)

			if result != nil && result.TreePath != nil && result.Hops != nil {
				return result, nil
			}

			if result != nil && result.TreePath != nil && graceTicks < 0 {
				graceTicks = hopsGracePeriod
				o.Lookup(key)
				lastLookup = time.Now()
			}

			if graceTicks > 0 {
				graceTicks--
			} else if graceTicks == 0 {
				if result == nil {
					result = o.collect(key)
				}
				if result != nil {
					return result, nil
				}
				graceTicks = -1
			}

			if time.Since(lastLookup) >= lookupRetryEvery {
				o.Lookup(key)
				lastLookup = time.Now()
			}
		}
	}
}

// //

// collect attempts to gather data from both tree and pathfinder sources.
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
