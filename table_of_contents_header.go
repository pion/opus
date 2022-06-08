package main

type (
	// The table-of-contents (TOC) header that signals which of the
	// various modes and configurations a given packet uses.  It is composed
	// of a configuration number, "config", a stereo flag, "s", and a frame
	// count code, "c", arranged as illustrated in Figure 1
	//
	//            0 1 2 3 4 5 6 7
	//           +-+-+-+-+-+-+-+-+
	//           | config  |s| c |
	//           +-+-+-+-+-+-+-+-+
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
	tableOfContentsHeader byte

	// The configuration numbers in each range (e.g., 0...3 for NB SILK-
	// only) correspond to the various choices of frame size, in the same
	// order.  For example, configuration 0 has a 10 ms frame size and
	// configuration 3 has a 60 ms frame size.
	// +-----------------------+-----------+-----------+-------------------+
	// | Configuration         | Mode      | Bandwidth | Frame Sizes       |
	// | Number(s)             |           |           |                   |
	// +-----------------------+-----------+-----------+-------------------+
	// | 0...3                 | SILK-only | NB        | 10, 20, 40, 60 ms |
	// |                       |           |           |                   |
	// | 4...7                 | SILK-only | MB        | 10, 20, 40, 60 ms |
	// |                       |           |           |                   |
	// | 8...11                | SILK-only | WB        | 10, 20, 40, 60 ms |
	// |                       |           |           |                   |
	// | 12...13               | Hybrid    | SWB       | 10, 20 ms         |
	// |                       |           |           |                   |
	// | 14...15               | Hybrid    | FB        | 10, 20 ms         |
	// |                       |           |           |                   |
	// | 16...19               | CELT-only | NB        | 2.5, 5, 10, 20 ms |
	// |                       |           |           |                   |
	// | 20...23               | CELT-only | WB        | 2.5, 5, 10, 20 ms |
	// |                       |           |           |                   |
	// | 24...27               | CELT-only | SWB       | 2.5, 5, 10, 20 ms |
	// |                       |           |           |                   |
	// | 28...31               | CELT-only | FB        | 2.5, 5, 10, 20 ms |
	// +-----------------------+-----------+-----------+-------------------+
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
	configuration byte

	// As described, the LP (SILK) layer and MDCT (CELT) layer can be
	// combined in three possible operating modes:
	// 1.  A SILK-only mode for use in low bitrate connections with an audio
	//     bandwidth of WB or less,
	//
	// 2.  A Hybrid (SILK+CELT) mode for SWB or FB speech at medium
	//     bitrates, and
	//
	// 3.  A CELT-only mode for very low delay speech transmission as well
	//     as music transmission (NB to FB).
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
	configurationMode byte

	// Opus can encode frames of 2.5, 5, 10, 20, 40, or 60 ms.  It can also
	// combine multiple frames into packets of up to 120 ms.  For real-time
	// applications, sending fewer packets per second reduces the bitrate,
	// since it reduces the overhead from IP, UDP, and RTP headers.
	// However, it increases latency and sensitivity to packet losses, as
	// losing one packet constitutes a loss of a bigger chunk of audio.
	// Increasing the frame duration also slightly improves coding
	// efficiency, but the gain becomes small for frame sizes above 20 ms.
	// For this reason, 20 ms frames are a good choice for most
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-2.1.4
	frameDuration byte

	// The Opus codec scales from 6 kbit/s narrowband mono speech to
	// 510 kbit/s fullband stereo music, with algorithmic delays ranging
	// from 5 ms to 65.2 ms.  At any given time, either the LP layer, the
	// MDCT layer, or both, may be active.  It can seamlessly switch between
	// all of its various operating modes, giving it a great deal of
	// flexibility to adapt to varying content and network conditions
	// without renegotiating the current session.  The codec allows input
	// and output of various audio bandwidths, defined as follows:

	// +----------------------+-----------------+-------------------------+
	// | Abbreviation         | Audio Bandwidth | Sample Rate (Effective) |
	// +----------------------+-----------------+-------------------------+
	// | NB (narrowband)      |           4 kHz |                   8 kHz |
	// |                      |                 |                         |
	// | MB (medium-band)     |           6 kHz |                  12 kHz |
	// |                      |                 |                         |
	// | WB (wideband)        |           8 kHz |                  16 kHz |
	// |                      |                 |                         |
	// | SWB (super-wideband) |          12 kHz |                  24 kHz |
	// |                      |                 |                         |
	// | FB (fullband)        |      20 kHz (*) |                  48 kHz |
	// +----------------------+-----------------+-------------------------+
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-2
	bandwidth byte
)

func (t tableOfContentsHeader) configuration() configuration {
	return configuration(t >> 3)
}

func (t tableOfContentsHeader) isStereo() bool {
	return (t & 0b00000100) != 0
}

func (t tableOfContentsHeader) numberOfFrames() byte {
	return 0
}

const (
	configurationModeSilkOnly configurationMode = iota + 1
	configurationModeCELTOnly
	configurationModeHybrid
)

func (c configurationMode) String() string {
	switch c {
	case configurationModeSilkOnly:
		return "Silk-only"
	case configurationModeCELTOnly:
		return "CELT-only"
	case configurationModeHybrid:
		return "Hybrid"
	}
	return "Invalid"
}

func (c configuration) mode() configurationMode {
	switch {
	case c >= 0 && c <= 11:
		return configurationModeSilkOnly
	case c >= 12 && c <= 15:
		return configurationModeHybrid
	case c >= 16 && c <= 31:
		return configurationModeCELTOnly
	default:
		return 0
	}
}

const (
	frameDuration2500us frameDuration = iota + 1
	frameDuration5ms
	frameDuration10ms
	frameDuration20ms
	frameDuration40ms
	frameDuration60ms
)

func (f frameDuration) String() string {
	switch f {
	case frameDuration2500us:
		return "2.5ms"
	case frameDuration5ms:
		return "5ms"
	case frameDuration10ms:
		return "10ms"
	case frameDuration20ms:
		return "20ms"
	case frameDuration40ms:
		return "40ms"
	case frameDuration60ms:
		return "60ms"
	}

	return "Invalid"
}

func (c configuration) frameDuration() frameDuration {
	switch c {
	case 16, 20, 24, 28:
		return frameDuration2500us
	case 17, 21, 25, 29:
		return frameDuration5ms
	case 0, 4, 8, 12, 14, 18, 22, 26, 30:
		return frameDuration10ms
	case 1, 5, 9, 13, 15, 19, 23, 27, 31:
		return frameDuration20ms
	case 2, 6:
		return frameDuration40ms
	case 3, 7, 11:
		return frameDuration60ms
	}

	return 0
}

const (
	bandwidthNarrowband bandwidth = iota + 1
	bandwidthMediumband
	bandwidthWideband
	bandwidthSuperwideband
	bandwidthFullband
)

func (c configuration) bandwidth() bandwidth {
	switch {
	case c <= 3:
		return bandwidthNarrowband
	case c <= 7:
		return bandwidthMediumband
	case c <= 11:
		return bandwidthWideband
	case c <= 13:
		return bandwidthSuperwideband
	case c <= 15:
		return bandwidthFullband
	case c <= 19:
		return bandwidthNarrowband
	case c <= 23:
		return bandwidthWideband
	case c <= 27:
		return bandwidthSuperwideband
	case c <= 31:
		return bandwidthFullband
	}

	return 0
}

func (b bandwidth) String() string {
	switch b {
	case bandwidthNarrowband:
		return "Narrowband"
	case bandwidthMediumband:
		return "Mediumband"
	case bandwidthWideband:
		return "Wideband"
	case bandwidthSuperwideband:
		return "Superwideband"
	case bandwidthFullband:
		return "Fullband"
	}
	return "Invalid"
}
