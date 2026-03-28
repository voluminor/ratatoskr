package gocmd

import (
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// Run dispatches the active command from the go trigger group.
// Returns (true, err) if a command was handled, (false, nil) otherwise.
func Run(cfg *gsettings.GoObj) (bool, error) {

	if cfg.Keygen > 0 {
		return true, keygen(cfg.Keygen)
	}

	return false, nil
}
