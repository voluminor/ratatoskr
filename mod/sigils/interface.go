package sigils

// // // // // // // // // //

// Interface defines one cloneable NodeInfo schema and its current data.
type Interface interface {
	// GetName returns the unique sigil name.
	GetName() string
	// GetParams returns the top-level NodeInfo keys owned by the sigil.
	GetParams() []string

	// ParseParams extracts owned keys from foreign NodeInfo and may update the receiver.
	ParseParams(map[string]any) map[string]any

	// Match reports whether foreign NodeInfo satisfies the sigil schema.
	Match(map[string]any) bool

	// Params returns an independent NodeInfo fragment containing current data.
	Params() map[string]any

	// Clone returns an independent copy of the schema and current data.
	Clone() Interface
}
