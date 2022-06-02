package main

//TODO right name?
type bandType int

const (
	narrowBandType bandType = iota + 1
	mediumBandType
	wideBandType
	superWideBandType
	fullBandType
)
