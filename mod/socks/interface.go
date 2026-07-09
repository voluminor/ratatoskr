package socks

// // // // // // // // // //

// Interface — SOCKS5 server contract
type Interface interface {
	Close() error
	Addr() string
	IsUnix() bool
	IsEnabled() bool
	MaxConnections() int
	SetMaxConnections(n int)
	ActiveConnections() int
}

// CredentialsInterface validates SOCKS5 username/password pairs.
type CredentialsInterface interface {
	Valid(user, password, userAddr string) bool
}
