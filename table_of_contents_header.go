// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

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

	// Configuration numbers in each range (e.g., 0...3 for NB SILK-
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
	Configuration byte

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

	// The Bandwidth the Opus codec scales from 6 kbit/s narrowband mono speech to
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
	Bandwidth byte

	// The remaining two bits of the TOC byte, labeled "c", code the number
	// of frames per packet (codes 0 to 3) as follows:

	// o  0: 1 frame in the packet

	// o  1: 2 frames in the packet, each with equal compressed size

	// o  2: 2 frames in the packet, with different compressed sizes

	// o  3: an arbitrary number of frames in the packet
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
	frameCode byte
)

func (t tableOfContentsHeader) configuration() Configuration {
	return Configuration(t >> 3)
}

func (t tableOfContentsHeader) isStereo() bool {
	return (t & 0b00000100) != 0
}

// nolint: deadcode, varcheck
const (
	frameCodeOneFrame           frameCode = 0
	frameCodeTwoEqualFrames     frameCode = 1
	frameCodeTwoDifferentFrames frameCode = 2
	frameCodeArbitraryFrames    frameCode = 3
)

func (t tableOfContentsHeader) frameCode() frameCode {
	return frameCode(t & 0b00000011)
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
	return "Invalid Configuration Mode"
}

// See Configuration for mapping of mode to configuration numbers
// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
func (c Configuration) mode() configurationMode {
	switch {
	case c > 0 && c <= 11:
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

	return "Invalid Frame Duration"
}

func (f frameDuration) nanoseconds() int {
	switch f {
	case frameDuration2500us:
		return 2500
	case frameDuration5ms:
		return 5000000
	case frameDuration10ms:
		return 10000000
	case frameDuration20ms:
		return 20000000
	case frameDuration40ms:
		return 40000000
	case frameDuration60ms:
		return 60000000
	}

	return 0
}

// See Configuration for mapping of frameDuration to configuration numbers
// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
func (c Configuration) frameDuration() frameDuration {
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

// Bandwidth constants
const (
	BandwidthNarrowband Bandwidth = iota + 1
	BandwidthMediumband
	BandwidthWideband
	BandwidthSuperwideband
	BandwidthFullband
)

// See Configuration for mapping of bandwidth to configuration numbers
// https://datatracker.ietf.org/doc/html/rfc6716#section-3.1
func (c Configuration) bandwidth() Bandwidth {
	switch {
	case c <= 3:
		return BandwidthNarrowband
	case c <= 7:
		return BandwidthMediumband
	case c <= 11:
		return BandwidthWideband
	case c <= 13:
		return BandwidthSuperwideband
	case c <= 15:
		return BandwidthFullband
	case c <= 19:
		return BandwidthNarrowband
	case c <= 23:
		return BandwidthWideband
	case c <= 27:
		return BandwidthSuperwideband
	case c <= 31:
		return BandwidthFullband
	}

	return 0
}

func (b Bandwidth) String() string {
	switch b {
	case BandwidthNarrowband:
		return "Narrowband"
	case BandwidthMediumband:
		return "Mediumband"
	case BandwidthWideband:
		return "Wideband"
	case BandwidthSuperwideband:
		return "Superwideband"
	case BandwidthFullband:
		return "Fullband"
	}
	return "Invalid Bandwidth"
}

// SampleRate returns the effective SampleRate for a given bandwidth
func (b Bandwidth) SampleRate() int {
	switch b {
	case BandwidthNarrowband:
		return 8000
	case BandwidthMediumband:
		return 12000
	case BandwidthWideband:
		return 16000
	case BandwidthSuperwideband:
		return 24000
	case BandwidthFullband:
		return 48000
	}
	return 0
}

// The TOC byte is followed by a byte encoding the number of frames in
// the packet in bits 2 to 7 (marked "M" in Figure 5), with bit 1 indicating
// whether or not Opus padding is inserted (marked "p" in Figure 5), and bit 0
// indicating VBR (marked "v" in Figure 5).  M MUST NOT be zero, and the audio
// duration contained within a packet MUST NOT exceed 120 ms [R5].  This
// limits the maximum frame count for any frame size to 48 (for 2.5 ms
// frames), with lower limits for longer frame sizes.  Figure 5
// illustrates the layout of the frame count byte.
//
//	        0
//	        0 1 2 3 4 5 6 7
//	       +-+-+-+-+-+-+-+-+
//	       |v|p|     M     |
//	       +-+-+-+-+-+-+-+-+
//
//	Figure 5: The frame count byte
//
// nolint: deadcode, unused
func parseFrameCountByte(in byte) (isVBR bool, hasPadding bool, frameCount byte) {
	isVBR = (in & 0b10000000) == 1
	hasPadding = (in & 0b01000000) == 1
	frameCount = in & 0b00111111
	return
}
