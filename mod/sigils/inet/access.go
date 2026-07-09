package inet

// // // // // // // // // //

// Addrs returns a defensive copy of the address list.
// The result is independent of internal state and safe to mutate.
func (o *Obj) Addrs() []string {
	return cloneAddrs(o.addrs)
}
