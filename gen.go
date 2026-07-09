package ratatoskr

//go:generate bash -c "rm -rf target/* tmp/*"
//go:generate bash -c "go run github.com/amazing-generators/gometagen/cmd/gometagen@$DOLLAR{GOMETAGEN_VERSION:-latest} generate -source _run/values.yml -hash-source . -hash-exclude .git -hash-exclude .idea -hash-exclude target -hash-exclude tmp -hash-exclude cmd/ratatoskr/target -hash-exclude cmd/ratatoskr/tmp -format go -out target/meta_gen.go -pkg target -force"
//go:generate bash -c "go run github.com/amazing-generators/godepsgen/cmd/godepsgen@$DOLLAR{GODEPSGEN_VERSION:-latest} -source . -out target/dependencies_gen.go -pkg target -skip-licenses -force"
//go:generate go run ./_generate/sigils
