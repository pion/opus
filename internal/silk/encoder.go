// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "github.com/pion/opus/internal/rangecoding"

// Encoder quantizes and range-encodes a single SILK channel. It is the
// counterpart to Decoder and is built up one stage at a time.
//
// State fields match the corresponding Decoder fields and use the same reset
// values so the encoder and decoder stay in sync across an uncoded frame.
type Encoder struct {
	rangeEncoder rangecoding.Encoder

	// haveEncoded reports whether a frame has been encoded yet; it selects
	// independent gain coding for the first subframe of the first frame.
	haveEncoded bool

	// previousLogGain is the running quantized log-gain index carried across
	// subframes and frames.
	previousLogGain int32

	// previousLag and isPreviousFrameVoiced carry pitch state across frames,
	// selecting relative vs absolute primary-lag coding.
	previousLag           int
	isPreviousFrameVoiced bool

	// firstFrameAfterReset caps the predictor more aggressively on the first
	// frame after a reset (find_pred_coefs).
	firstFrameAfterReset bool

	// prevNLSFq holds the previous frame's quantized NLSFs (Q15) for LSF
	// interpolation.
	prevNLSFq []int16

	// vadFlags holds the per-unit VAD decision of the in-progress packet,
	// indexed by unit. The header interval is reserved as zero before the
	// units are coded and patched from these flags after the last unit,
	// mirroring libopus's VAD_flags[] plus ec_enc_patch_initial_bits.
	vadFlags [3]bool

	// Analysis state for the frame encoder.
	vad               vadState
	nsq               *nsqState
	frameCounter      int
	targetBitrate     int       // target bitrate in bps (drives control_SNR)
	packetLossPerc    int       // expected packet loss %, drives LTP state scaling
	sumLogGainQ7      int32     // cumulative LTP gain limit (quant_LTP_gains)
	xBuf              []float32 // previous frame, as LTP-memory history for pitch analysis
	ltpCorr           float32   // normalized correlation carried across frames
	tiltSmth          float32   // smoothed spectral tilt (shape state)
	harmShapeGainSmth float32   // smoothed harmonic shaping gain (shape state)

	// useInterpolatedNLSFs mirrors libopus's psEncC->useInterpolatedNLSFs
	// (silk_setup_complexity, control_codec.c): NLSF interpolation search
	// only runs at encoder complexity >= 4. Set via SetUseInterpolatedNLSFs;
	// defaults to false (matching the low-complexity tiers) until the caller
	// configures it.
	useInterpolatedNLSFs bool
}

// NewEncoder creates a SILK Encoder with its prediction state reset.
func NewEncoder() Encoder {
	e := Encoder{vad: newVADState(), nsq: newNSQState()}
	e.resetPredictionState()

	return e
}

// Clone returns an independent copy of the persistent encoder state. The
// range coder is deliberately reset instead of copied: Encode initializes it
// at the start of every packet, and sharing its backing buffers would let a
// speculative encode modify the original encoder.
func (e *Encoder) Clone() Encoder {
	clone := *e
	clone.rangeEncoder = rangecoding.Encoder{}
	clone.prevNLSFq = append([]int16(nil), e.prevNLSFq...)
	clone.xBuf = append([]float32(nil), e.xBuf...)
	if e.nsq != nil {
		nsqClone := *e.nsq
		nsqClone.xq = append([]int16(nil), e.nsq.xq...)
		nsqClone.sLTPShpQ14 = append([]int32(nil), e.nsq.sLTPShpQ14...)
		nsqClone.sLPCQ14 = append([]int32(nil), e.nsq.sLPCQ14...)
		clone.nsq = &nsqClone
	}

	return clone
}

// SetUseInterpolatedNLSFs enables or disables the NLSF interpolation search
// in findLPCNLSF, mirroring libopus's complexity-tier setting
// (silk_setup_complexity: enabled for encoder complexity >= 4).
func (e *Encoder) SetUseInterpolatedNLSFs(enabled bool) {
	e.useInterpolatedNLSFs = enabled
}

// resetPredictionState resets the encoder prediction state. The values must
// match Decoder.resetPredictionState.
func (e *Encoder) resetPredictionState() {
	e.haveEncoded = false
	e.previousLogGain = 10
	e.previousLag = 100
	e.isPreviousFrameVoiced = false
	e.firstFrameAfterReset = true
	e.sumLogGainQ7 = 0
	e.vadFlags = [3]bool{}
	e.prevNLSFq = make([]int16, maxLPCOrder)
	if e.targetBitrate == 0 {
		e.targetBitrate = 24000
	}
}
