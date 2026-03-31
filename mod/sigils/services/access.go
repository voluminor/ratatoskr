package services

// // // // // // // // // //

// Services returns the service→port map.
func (o *Obj) Services() map[string]uint16 {
	return o.services
}
