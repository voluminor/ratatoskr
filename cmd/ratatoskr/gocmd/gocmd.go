package gocmd

import (
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// Run dispatches the active command from the go trigger group.
// Returns (true, err) if a command was handled, (false, nil) otherwise.
func Run(cfg *gsettings.GoObj) (bool, error) {
	if handled, err := keysCmd(&cfg.Keys); handled {
		return true, err
	}

	if handled, err := confCmd(&cfg.Conf); handled {
		return true, err
	}

	if handled, err := peerInfoCmd(&cfg.PeerInfo); handled {
		return true, err
	}

	if handled, err := forwardCmd(&cfg.Forward); handled {
		return true, err
	}

	if cfg.Traceroute.Scan || cfg.Traceroute.Trace != "" || cfg.Traceroute.Ping != "" {
		return true, traceCmd(&cfg.Traceroute)
	}

	return false, nil
}
