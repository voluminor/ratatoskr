package core

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

func buildCoreOptions(cfg *config.NodeConfig) ([]yggcore.SetupOption, error) {
	n := 2 + len(cfg.Listen) + len(cfg.Peers) + len(cfg.AllowedPublicKeys)
	for _, peers := range cfg.InterfacePeers {
		n += len(peers)
	}
	opts := make([]yggcore.SetupOption, 0, n)
	opts = append(opts, yggcore.NodeInfo(cfg.NodeInfo))
	opts = append(opts, yggcore.NodeInfoPrivacy(cfg.NodeInfoPrivacy))
	for _, addr := range cfg.Listen {
		opts = append(opts, yggcore.ListenAddress(addr))
	}
	for _, peer := range cfg.Peers {
		opts = append(opts, yggcore.Peer{URI: peer})
	}
	for intf, peers := range cfg.InterfacePeers {
		for _, peer := range peers {
			opts = append(opts, yggcore.Peer{URI: peer, SourceInterface: intf})
		}
	}
	for _, allowed := range cfg.AllowedPublicKeys {
		k, err := hex.DecodeString(allowed)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrInvalidAllowedPublicKey, allowed, err)
		}
		if len(k) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w %q: got %d bytes, expected %d", ErrInvalidAllowedPublicKey, allowed, len(k), ed25519.PublicKeySize)
		}
		opts = append(opts, yggcore.AllowedPublicKey(k))
	}
	return opts, nil
}
