package ninfo

import "github.com/voluminor/ratatoskr/target"

// // // // // // // // // //

func reservedSigilName(name string) bool {
	_, ok := target.Parse(name)
	return ok
}
