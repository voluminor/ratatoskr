package inet

// // // // // // // // // //

// Addrs returns an independent copy of the address list.
func (o *Obj) Addrs() []string {
	return cloneAddrs(o.addrs)
}
