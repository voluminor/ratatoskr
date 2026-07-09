package public

// // // // // // // // // //

// Peers returns a defensive copy of the group→URIs map.
// The result is independent of internal state and safe to mutate.
func (o *Obj) Peers() map[string][]string {
	return clonePeers(o.peers)
}
