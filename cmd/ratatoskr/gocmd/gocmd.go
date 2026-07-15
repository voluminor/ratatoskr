// Package gocmd executes parsed CLI utility commands.
package gocmd

import (
	gsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/gsettings"
)

// // // // // // // // // //

// Run executes the selected utility command and reports whether one was set.
func Run(cfg *gsettings.GoObj) (bool, error) {
	if handled, err := keyCmd(&cfg.Key); handled {
		return true, err
	}

	if handled, err := confCmd(&cfg.Conf); handled {
		return true, err
	}

	if handled, err := askCmd(&cfg.Ask); handled {
		return true, err
	}

	if handled, err := peerInfoCmd(&cfg.PeerInfo); handled {
		return true, err
	}

	if handled, err := forwardCmd(&cfg.Forward); handled {
		return true, err
	}

	if cfg.Probe.Scan || cfg.Probe.Trace != "" || cfg.Probe.Ping != "" {
		return true, traceCmd(&cfg.Probe)
	}

	return false, nil
}
