package socks

// // // // // // // // // //

// CredentialsInterface validates SOCKS5 username/password pairs.
type CredentialsInterface interface {
	Valid(user, password, userAddr string) bool
}
