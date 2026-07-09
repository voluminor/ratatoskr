package services

// // // // // // // // // //

// Services returns a defensive copy of the service→port map.
// The result is independent of internal state and safe to mutate.
func (o *Obj) Services() map[string]uint16 {
	return cloneServices(o.services)
}
