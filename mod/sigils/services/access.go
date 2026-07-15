package services

// // // // // // // // // //

// Services returns an independent copy of the service map.
func (o *Obj) Services() map[string]uint16 {
	return cloneServices(o.services)
}
