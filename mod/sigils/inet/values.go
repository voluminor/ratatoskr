package inet

import "regexp"

// // // // // // // // // //

const sigName = "inet"

var sigKeys = []string{sigName}

const maxAddrs = 32

var reAddr = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{4,256}$`)
