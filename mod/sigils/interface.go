package sigils

import "regexp"

// // // // // // // // // //

// Interface defines a typed block of NodeInfo data.
// Each sigil owns one or more top-level keys in the NodeInfo map.
type Interface interface {
	GetName() string
	GetParams() []string

	// SetParams writes sigil data into a copy of NodeInfo; never mutates the input.
	SetParams(map[string]any) (map[string]any, error)

	// ParseParams extracts this sigil's keys from foreign NodeInfo
	// and stores the result into the object for later retrieval via Params.
	ParseParams(map[string]any) map[string]any

	// Match checks whether foreign NodeInfo contains this sigil
	// with correct structure and JSON types.
	Match(map[string]any) bool

	// Params returns the sigil's current data as a NodeInfo fragment.
	Params() map[string]any

	// Clone returns a deep copy of the sigil with its current state.
	// Allows a single Interface value to act as both a contract
	// and a data carrier for third-party sigils.
	Clone() Interface
}

// //

var reName = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

func ValidateName(name string) bool {
	return reName.MatchString(name)
}
