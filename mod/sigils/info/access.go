package info

// // // // // // // // // //

// Info returns a defensive copy of the config, including a deep-copied
// Contacts map. The result is independent of internal state and safe to mutate.
func (o *Obj) Info() *ConfigObj {
	if o.conf == nil {
		return nil
	}
	c := cloneConfig(o.conf)
	return &c
}
