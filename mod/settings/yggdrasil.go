package settings

import (
	"encoding/hex"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// NodeConfig maps yggdrasil settings into config.NodeConfig.
// Converts key.text from hex to bytes; all other processing
// (key.path, certificate generation) is handled by the core.
func NodeConfig(s YggdrasilInterface) (*config.NodeConfig, error) {
	y := s.Self().(*gsettings.YggdrasilObj)

	cfg := &config.NodeConfig{
		PrivateKeyPath:    y.Key.Path,
		Listen:            y.Listen,
		AllowedPublicKeys: y.AllowedPublicKeys,
		AdminListen:       y.AdminListen,
		IfName:            y.If.Name,
		IfMTU:             y.If.Mtu,
		NodeInfo:          y.Node.Info,
		NodeInfoPrivacy:   y.Node.Privacy,
		LogLookups:        y.LogLookups,
		InterfacePeers:    y.Peers.Interface,
	}

	if y.Peers.Manager.Enable {
		cfg.Peers = nil
	} else {
		cfg.Peers = y.Peers.Url
	}

	if cfg.InterfacePeers == nil {
		cfg.InterfacePeers = map[string][]string{}
	}

	if y.Key.Text != "" {
		key, err := hex.DecodeString(y.Key.Text)
		if err != nil {
			return nil, fmt.Errorf("invalid key.text hex: %w", err)
		}
		cfg.PrivateKey = key
	}

	if y.Multicast.Regex != "" {
		cfg.MulticastInterfaces = []config.MulticastInterfaceConfig{{
			Regex:    y.Multicast.Regex,
			Beacon:   y.Multicast.Beacon,
			Listen:   y.Multicast.Listen,
			Port:     y.Multicast.Port,
			Priority: uint64(y.Multicast.Priority),
			Password: y.Multicast.Password,
		}}
	}

	return cfg, nil
}

// //

// FromNodeConfig populates yggdrasil settings from a config.NodeConfig.
// Takes a base settings Interface, clones it, fills the yggdrasil branch
// and returns the updated Interface.
func FromNodeConfig(cfg *config.NodeConfig, base Interface) Interface {
	obj := *Obj(base)
	y := &obj.Yggdrasil

	if len(cfg.PrivateKey) > 0 {
		y.Key.Text = hex.EncodeToString(cfg.PrivateKey)
	}
	y.Key.Path = cfg.PrivateKeyPath
	y.Listen = cfg.Listen
	y.Peers.Url = cfg.Peers
	y.Peers.Interface = cfg.InterfacePeers
	y.AllowedPublicKeys = cfg.AllowedPublicKeys
	y.AdminListen = cfg.AdminListen
	y.If.Name = cfg.IfName
	y.If.Mtu = cfg.IfMTU
	y.Node.Info = cfg.NodeInfo
	y.Node.Privacy = cfg.NodeInfoPrivacy
	y.LogLookups = cfg.LogLookups

	if len(cfg.MulticastInterfaces) > 0 {
		m := cfg.MulticastInterfaces[0]
		y.Multicast.Regex = m.Regex
		y.Multicast.Beacon = m.Beacon
		y.Multicast.Listen = m.Listen
		y.Multicast.Port = m.Port
		y.Multicast.Priority = uint16(m.Priority)
		y.Multicast.Password = m.Password
	}

	return &obj
}
