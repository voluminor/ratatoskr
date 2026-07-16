package main

//go:generate bash -c "rm -rf target/* tmp/*"
//go:generate bash -c "go run github.com/amazing-generators/goconfgen/cmd/goconfgen@$DOLLAR{GOCONFGEN_VERSION:-latest} -source yml/config -out target/settings -pkg settings -formats yaml,json,hjson -force"
