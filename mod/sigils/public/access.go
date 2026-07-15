package public

// // // // // // // // // //

// Peers returns an independent copy of the grouped URI map.
func (o *Obj) Peers() map[string][]string {
	return clonePeers(o.peers)
}
