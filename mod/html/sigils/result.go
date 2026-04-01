package sigils

// // // // // // // // // //

// ResultObj holds rendered HTML blocks mirroring sigil_core.Obj layout.
type ResultObj struct {
	LocalNodeInfo []byte
	Sigils        map[string][]byte
}
