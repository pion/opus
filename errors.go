package main

import "errors"

var (
	errTooShortForTableOfContentsHeader = errors.New("Packet is too short to contain table of contents header")
)
