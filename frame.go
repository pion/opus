package main

func parsePacket(in []byte) (c configuration, isStereo bool, frames [][]byte, err error) {
	if len(in) < 1 {
		err = errTooShortForTableOfContentsHeader
		return
	}

	tocHeader := tableOfContentsHeader(in[0])
	c = tocHeader.configuration()
	isStereo = tocHeader.isStereo()

	return
}
