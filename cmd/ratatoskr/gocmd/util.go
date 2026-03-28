package gocmd

import (
	"fmt"
	"os"
	"time"
)

// // // // // // // // // //

var spinnerFrames = [...]rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// //

func formatRemaining(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := d.Seconds()
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	return fmt.Sprintf("%dm%02ds", int(s)/60, int(s)%60)
}

// //

func clearLine() {
	fmt.Fprint(os.Stderr, "\r\033[2K")
}
