package services

import "regexp"

// // // // // // // // // //

const sigName = "services"

var sigKeys = []string{sigName}

const maxServices = 256

var reServiceName = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
