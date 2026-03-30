package info

import "regexp"

// // // // // // // // // //

const sigName = "info"

var sigKeys = []string{
	"name",
	"type",
	"location",
	"contact",
	"description",
}

const (
	maxContactGroups    = 8
	maxContactsPerGroup = 8
)

var (
	reName     = regexp.MustCompile(`^[a-z0-9._-]{4,64}$`)
	reType     = regexp.MustCompile(`^[a-z0-9.-]{2,32}$`)
	reText     = regexp.MustCompile(`^\S[\S ]{0,512}\S$`)
	reContacts = regexp.MustCompile(`^\S[\S ]{1,256}\S$`)
)
