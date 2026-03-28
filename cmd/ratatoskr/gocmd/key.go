package gocmd

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

var (
	reHexPrivate = regexp.MustCompile(`^[0-9a-fA-F]{128}$`)
	reHexPublic  = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// //

func keyCmd(cfg *gsettings.GoKeyObj) (bool, error) {
	if cfg.Gen > 0 {
		return true, keygen(cfg.Gen)
	}
	if cfg.ToPem != "" {
		return true, keyToPem(cfg)
	}
	if cfg.FromPem != "" {
		return true, keyFromPem(cfg.FromPem)
	}
	if cfg.Addr != "" {
		return true, keyAddr(cfg.Addr)
	}
	return false, nil
}

// //

func keyAddr(input string) error {
	pub, err := resolvePublicKey(input)
	if err != nil {
		return err
	}

	addr := address.AddrForKey(pub)
	subnet := address.SubnetForKey(pub)
	if addr == nil || subnet == nil {
		return fmt.Errorf("failed to derive address from key")
	}

	ip := net.IP(addr[:])

	var snIP [16]byte
	copy(snIP[:], subnet[:])
	sn := net.IPNet{
		IP:   net.IP(snIP[:]),
		Mask: net.CIDRMask(64, 128),
	}

	fmt.Printf("address: %s\n", ip)
	fmt.Printf("subnet:  %s\n", sn.String())
	fmt.Printf("key:     %s\n", hex.EncodeToString(pub))
	return nil
}

// //

func keyToPem(cfg *gsettings.GoKeyObj) error {
	if cfg.Addr == "" {
		return fmt.Errorf("specify -go.key.addr with hex private key (128 chars) or PEM file path")
	}

	priv, err := resolvePrivateKey(cfg.Addr)
	if err != nil {
		return err
	}

	nodeCfg := &config.NodeConfig{PrivateKey: config.KeyBytes(priv)}
	pemData, err := nodeCfg.MarshalPEMPrivateKey()
	if err != nil {
		return fmt.Errorf("marshal PEM: %w", err)
	}

	if err := os.WriteFile(cfg.ToPem, pemData, 0600); err != nil {
		return fmt.Errorf("write PEM file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", cfg.ToPem)
	return nil
}

// //

func keyFromPem(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read PEM file: %w", err)
	}

	nodeCfg := &config.NodeConfig{}
	if err := nodeCfg.UnmarshalPEMPrivateKey(data); err != nil {
		return fmt.Errorf("parse PEM: %w", err)
	}

	fmt.Println(hex.EncodeToString(nodeCfg.PrivateKey))
	return nil
}

// //

func resolvePublicKey(input string) (ed25519.PublicKey, error) {
	switch {
	case reHexPrivate.MatchString(input):
		raw, _ := hex.DecodeString(input)
		priv := ed25519.PrivateKey(raw)
		return priv.Public().(ed25519.PublicKey), nil

	case reHexPublic.MatchString(input):
		raw, _ := hex.DecodeString(input)
		return ed25519.PublicKey(raw), nil

	default:
		priv, err := loadPEMPrivateKey(input)
		if err != nil {
			return nil, err
		}
		return priv.Public().(ed25519.PublicKey), nil
	}
}

// //

func resolvePrivateKey(input string) (ed25519.PrivateKey, error) {
	switch {
	case reHexPrivate.MatchString(input):
		raw, _ := hex.DecodeString(input)
		return ed25519.PrivateKey(raw), nil

	case reHexPublic.MatchString(input):
		return nil, fmt.Errorf("public key cannot be converted to private key")

	default:
		return loadPEMPrivateKey(input)
	}
}

// //

const minKeygenDuration = 100 * time.Millisecond

// //

type keygenResultObj struct {
	privateKey ed25519.PrivateKey
	ones       byte
}

// //

func keygen(duration time.Duration) error {
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

	animateProgress(&done, &total, duration)
	wg.Wait()

	if best.privateKey == nil {
		return nil, fmt.Errorf("key generation failed: no keys produced")
	}

	return &best, nil
}

// //

func animateProgress(done *atomic.Bool, total *atomic.Uint64, duration time.Duration) {
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

		fmt.Fprintf(os.Stderr, "\r%c mining key... %s remaining | %.1fM keys/sec  ",
			spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining), rate)
		frame++
	}
	ticker.Stop()

	done.Store(true)
	fmt.Fprint(os.Stderr, "\r\033[2K")
}

// //

func loadPEMPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("not a valid hex key and cannot read file: %w", err)
	}

	nodeCfg := &config.NodeConfig{}
	if err := nodeCfg.UnmarshalPEMPrivateKey(data); err != nil {
		return nil, fmt.Errorf("parse PEM: %w", err)
	}

	return ed25519.PrivateKey(nodeCfg.PrivateKey), nil
}
