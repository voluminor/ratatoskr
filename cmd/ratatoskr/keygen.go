package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// // // // // // // // // //

const minKeygenDuration = 100 * time.Millisecond

var spinnerFrames = [...]rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// //

type keygenResultObj struct {
	privateKey ed25519.PrivateKey
	ones       byte
	checked    uint64
}

// //

func handleKeygen(duration time.Duration) error {
	if duration < minKeygenDuration {
		duration = minKeygenDuration
	}

	result, err := mineKey(duration)
	if err != nil {
		return err
	}

	fmt.Println(hex.EncodeToString(result.privateKey))
	return nil
}

// //

func mineKey(duration time.Duration) (*keygenResultObj, error) {
	workers := runtime.NumCPU()

	var (
		bestMu sync.Mutex
		best   keygenResultObj
		total  atomic.Uint64
		done   atomic.Bool
	)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			cfg := &config.NodeConfig{}
			prefixLen := len(address.GetPrefix())
			var localBest byte

			for !done.Load() {
				cfg.NewPrivateKey()
				priv := ed25519.PrivateKey(cfg.PrivateKey)
				pub := priv.Public().(ed25519.PublicKey)
				ones := address.AddrForKey(pub)[prefixLen]
				total.Add(1)

				if ones <= localBest {
					continue
				}
				localBest = ones

				bestMu.Lock()
				if ones > best.ones {
					best.ones = ones
					best.privateKey = make(ed25519.PrivateKey, ed25519.PrivateKeySize)
					copy(best.privateKey, priv)
				}
				bestMu.Unlock()
			}
		}()
	}

	animateProgress(&done, &total, &bestMu, &best, duration)
	wg.Wait()

	best.checked = total.Load()
	if best.privateKey == nil {
		return nil, fmt.Errorf("key generation failed: no keys produced")
	}

	return &best, nil
}

// //

func animateProgress(done *atomic.Bool, total *atomic.Uint64, mu *sync.Mutex, best *keygenResultObj, duration time.Duration) {
	start := time.Now()
	deadline := start.Add(duration)
	frame := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	for now := range ticker.C {
		if now.After(deadline) {
			break
		}

		remaining := deadline.Sub(now)
		keys := total.Load()
		elapsed := now.Sub(start).Seconds()
		rate := 0.0
		if elapsed > 0 {
			rate = float64(keys) / elapsed / 1e6
		}

		mu.Lock()
		ones := best.ones
		mu.Unlock()

		fmt.Fprintf(os.Stderr, "\r%c mining key... %s remaining | best: %d leading zeros | %.1fM keys/sec  ",
			spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining), ones, rate)
		frame++
	}
	ticker.Stop()

	done.Store(true)
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))
}

// //

func formatRemaining(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := d.Seconds()
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	return fmt.Sprintf("%dm%02ds", int(s)/60, int(s)%60)
}
