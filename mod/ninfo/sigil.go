package ninfo

import "regexp"

// // // // // // // // // //

// SigilInterface is a typed block of NodeInfo data.
// Each sigil owns one or more top-level keys in the NodeInfo map.
// Sigils are used to build local NodeInfo from Go structs and to
// recognize and parse the same structure in foreign NodeInfo from JSON.
type SigilInterface interface {
	// GetName returns the unique sigil identifier (e.g. "info", "services").
	// Must be stable — same value on every call, validated by ValidateSigilName.
	GetName() string

	// GetParams returns the list of top-level NodeInfo keys this sigil owns.
	// Used by Obj.Add to detect conflicts and by Obj.Del to clean up.
	// Must be stable and match what SetParams actually writes.
	GetParams() []string

	// SetParams writes sigil data into a copy of the given NodeInfo.
	// Returns a new map with sigil keys added; never mutates the input.
	// On error the original map stays intact.
	// Zero-value fields (empty strings, nil maps) should be skipped.
	SetParams(map[string]any) (map[string]any, error)

	// ParseParams extracts only this sigil's keys from any NodeInfo.
	// Returns a new map with recognized keys; missing keys are omitted.
	// Never fails.
	ParseParams(map[string]any) map[string]any

	// Match checks whether a foreign NodeInfo contains this sigil.
	// Validates structure and types only, not content.
	// Foreign data comes from JSON: arrays are []any, maps are
	// map[string]any, numbers are float64.
	Match(map[string]any) bool
}

// //

var reSigilName = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

func ValidateSigilName(name string) bool {
	return reSigilName.MatchString(name)
}
