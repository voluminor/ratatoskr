package socks

// // // // // // // // // //

// Interface — контракт SOCKS5-сервера
type Interface interface {
	Enable(cfg EnableConfigObj) error
	Disable() error
	Addr() string
	IsUnix() bool
	IsEnabled() bool
}
