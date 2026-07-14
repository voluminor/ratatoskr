package ratatoskr

//go:generate bash -c "rm -rf target/* tmp/*"
//go:generate bash -c "go run github.com/amazing-generators/gometagen/cmd/gometagen@latest generate -source _run/values.yml -hash-source . -hash-exclude .git -hash-exclude .idea -hash-exclude target -hash-exclude tmp -hash-exclude cmd -format go -out target/meta_gen.go -pkg target -force"
//go:generate go run ./_generate/sigils
