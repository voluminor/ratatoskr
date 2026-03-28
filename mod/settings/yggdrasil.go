package settings

import (
	"encoding/hex"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// NodeConfig maps generated yggdrasil settings into config.NodeConfig.
// Converts private_key from hex to bytes; all other processing
// (private_key_path, certificate generation) is handled by the core.
func NodeConfig(s *gsettings.YggdrasilObj) (*config.NodeConfig, error) {
	cfg := &config.NodeConfig{
		PrivateKeyPath:    s.PrivateKeyPath,
		Peers:             s.Peers,
		Listen:            s.Listen,
		AllowedPublicKeys: s.AllowedPublicKeys,
		AdminListen:       s.AdminListen,
		IfName:            s.IfName,
		IfMTU:             s.IfMtu,
		NodeInfo:          s.NodeInfo,
		NodeInfoPrivacy:   s.NodeInfoPrivacy,
		LogLookups:        s.LogLookups,
		InterfacePeers:    s.InterfacePeers,
	}

	if cfg.InterfacePeers == nil {
		cfg.InterfacePeers = map[string][]string{}
	}

	if s.PrivateKey != "" {
		key, err := hex.DecodeString(s.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private_key hex: %w", err)
		}
		cfg.PrivateKey = key
	}

	if s.Multicast.Regex != "" {
		cfg.MulticastInterfaces = []config.MulticastInterfaceConfig{{
			Regex:    s.Multicast.Regex,
			Beacon:   s.Multicast.Beacon,
			Listen:   s.Multicast.Listen,
			Port:     s.Multicast.Port,
			Priority: uint64(s.Multicast.Priority),
			Password: s.Multicast.Password,
		}}
	}

	return cfg, nil
}
