package settings

// // // // // // // // // //

// Interface — top-level contract for a settings object
type Interface interface {
	GetConfig() string
	GetGenKey() bool
	GetSaveConfig() string
}
