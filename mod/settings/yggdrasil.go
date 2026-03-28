package settings

import (
	"encoding/hex"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// NodeConfig builds a config.NodeConfig from the yggdrasil settings branch.
//
// key.text is decoded from hex to raw bytes; an invalid hex string
// causes an error. When key.text is empty, PrivateKey stays nil and
// the core generates or loads a key via key.path.
//
// If peers.manager is enabled, cfg.Peers is set to nil so the core
// delegates peer discovery to the built-in manager instead of a static list.
func NodeConfig(s gsettings.YggdrasilInterface) (*config.NodeConfig, error) {
	cfg := &config.NodeConfig{
		PrivateKeyPath:    s.GetKey().GetPath(),
		Listen:            s.GetListen(),
		AllowedPublicKeys: s.GetAllowedPublicKeys(),
		AdminListen:       s.GetAdminListen(),
		IfName:            s.GetIf().GetName(),
		IfMTU:             s.GetIf().GetMtu(),
		NodeInfo:          s.GetNode().GetInfo(),
		NodeInfoPrivacy:   s.GetNode().GetPrivacy(),
		LogLookups:        s.GetLogLookups(),
		InterfacePeers:    s.GetPeers().GetInterface(),
	}

	if s.GetPeers().GetManager().GetEnable() {
		cfg.Peers = nil
	} else {
		cfg.Peers = s.GetPeers().GetUrl()
	}

	if cfg.InterfacePeers == nil {
		cfg.InterfacePeers = map[string][]string{}
	}

	keyText := s.GetKey().GetText()
	if keyText != "" {
		key, err := hex.DecodeString(keyText)
		if err != nil {
			return nil, fmt.Errorf("invalid key.text hex: %w", err)
		}
		cfg.PrivateKey = key
	}

	mc := s.GetMulticast()
	if mc.GetRegex() != "" {
		cfg.MulticastInterfaces = []config.MulticastInterfaceConfig{{
			Regex:    mc.GetRegex(),
			Beacon:   mc.GetBeacon(),
			Listen:   mc.GetListen(),
			Port:     mc.GetPort(),
			Priority: uint64(mc.GetPriority()),
			Password: mc.GetPassword(),
		}}
	}

	return cfg, nil
}

// //

// FromNodeConfig creates a new settings object by shallow-copying base and
// overwriting its yggdrasil branch with values from cfg.
//
// The base object is NOT mutated — the copy is stack-allocated and
// escapes to the heap via the returned pointer. Non-yggdrasil fields
// (e.g. log settings) are preserved from base.
//
// Only the first MulticastInterfaces entry is mapped; additional entries
// in cfg.MulticastInterfaces are silently ignored.
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
