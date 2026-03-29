package ninfo

import "regexp"

// // // // // // // // // //

type SigilInterface interface {
	GetName() string
	GetParams() []string

	ParseParams(map[string]any) map[string]any
	SetParams(map[string]any) (map[string]any, error)
	Match(map[string]any) bool
}

// //

var reSigilName = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

func ValidateSigilIName(name string) bool {
	return reSigilName.MatchString(name)
}
