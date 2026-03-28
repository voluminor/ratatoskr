package ratatoskr

//go:generate bash -c "rm -rf target/*"
//go:generate bash -c "rm -rf tmp/*"

//go:generate bash "./_run/scripts/go_creator_const.sh"
//go:generate go run ./_generate/dependencies
//go:generate go run ./_generate/settings
