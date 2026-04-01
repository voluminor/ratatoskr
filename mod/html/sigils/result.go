package sigils

// // // // // // // // // //

// ResultObj holds rendered HTML blocks from a parsed NodeInfo.
type ResultObj struct {
	Extra  []byte
	Sigils map[string][]byte
}
