package gocmd

import (
	"fmt"
	"os"
)

// // // // // // // // // //

// cliLoggerObj suppresses debug/info output but prints warnings and errors
// to stderr. Uses \r\033[2K before each line so the message appears cleanly
// above an active spinner without being overwritten.
type cliLoggerObj struct{}

// //

func (cliLoggerObj) Printf(string, ...interface{}) {}
func (cliLoggerObj) Println(...interface{})        {}
func (cliLoggerObj) Infof(string, ...interface{})  {}
func (cliLoggerObj) Infoln(...interface{})         {}
func (cliLoggerObj) Debugf(string, ...interface{}) {}
func (cliLoggerObj) Debugln(...interface{})        {}
func (cliLoggerObj) Traceln(...interface{})        {}

// //

func (cliLoggerObj) Warnf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "\r\033[2K[warn] "+f+"\n", a...)
}

func (cliLoggerObj) Warnln(a ...interface{}) {
	fmt.Fprint(os.Stderr, append([]interface{}{"\r\033[2K[warn] "}, a...)...)
	fmt.Fprintln(os.Stderr)
}

func (cliLoggerObj) Errorf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "\r\033[2K[error] "+f+"\n", a...)
}

func (cliLoggerObj) Errorln(a ...interface{}) {
	fmt.Fprint(os.Stderr, append([]interface{}{"\r\033[2K[error] "}, a...)...)
	fmt.Fprintln(os.Stderr)
}
