package socks

// // // // // // // // // //

// Interface — SOCKS5 server contract
type Interface interface {
	Enable(cfg EnableConfigObj) error
	Disable() error
	Addr() string
	IsUnix() bool
	IsEnabled() bool
}
