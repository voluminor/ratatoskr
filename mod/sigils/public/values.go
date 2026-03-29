package public

import "regexp"

// // // // // // // // // //

const sigName = "public"

var sigKeys = []string{sigName}

const (
	maxGroups       = 8
	maxURIsPerGroup = 16
)

var (
	reGroup = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
	reURI   = regexp.MustCompile(`^[a-zA-Z0-9+._/:@\[\]-]{8,256}$`)
)
