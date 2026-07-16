package info

// // // // // // // // // //

// Info returns an independent copy of the identity-card configuration.
func (o *Obj) Info() *ConfigObj {
	if o.conf == nil {
		return nil
	}
	c := cloneConfig(o.conf)
	return &c
}
