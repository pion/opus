// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

// softClipMemory carries the clipping non-linearity across frame boundaries so
// a peak that straddles two frames does not pick up a discontinuity.
type softClipMemory [2]float32

// softClip bends samples that overshoot [-1, 1] back inside it with a
// quadratic non-linearity instead of letting the int16 conversion square them
// off, which is audible as a click. Port of libopus opus_pcm_soft_clip_impl
// (src/opus.c); libopus applies it on the integer decode path only, leaving
// float callers free to handle headroom themselves.
//
// Samples are first clamped to [-2, 2], the domain the non-linearity is
// defined over — beyond it the derivative is zero, so clamping there
// introduces no discontinuity of its own.
//
//nolint:cyclop,gocognit // Mirrors the reference peak-scan and ramp structure.
func softClip(x []float32, channels int, mem *softClipMemory) {
	if channels < 1 || len(x) < channels {
		return
	}
	n := len(x) / channels

	for i := range x {
		x[i] = min(2, max(-2, x[i]))
	}

	for channel := range channels {
		at := func(i int) float32 { return x[i*channels+channel] }
		set := func(i int, v float32) { x[i*channels+channel] = v }

		curve := mem[channel]
		// Finish applying the previous frame's non-linearity.
		for i := range n {
			if at(i)*curve >= 0 {
				break
			}
			set(i, at(i)+curve*at(i)*at(i))
		}

		curr := 0
		x0 := at(0)
		for {
			i := curr
			for ; i < n; i++ {
				if at(i) > 1 || at(i) < -1 {
					break
				}
			}
			if i == n {
				curve = 0

				break
			}

			peakPos := i
			maxVal := absFloat32(at(i))
			start, end := i, i
			// Widen to the zero crossings on either side of the peak so the
			// correction is applied over a whole half-cycle.
			for start > 0 && at(i)*at(start-1) >= 0 {
				start--
			}
			for end < n && at(i)*at(end) >= 0 {
				if absFloat32(at(end)) > maxVal {
					maxVal = absFloat32(at(end))
					peakPos = end
				}
				end++
			}
			special := start == 0 && at(i)*at(0) >= 0

			// Pick the curve so that maxVal + curve*maxVal² == 1, nudged just enough that
			// rounding cannot leave a sample above 1.
			curve = (maxVal - 1) / (maxVal * maxVal)
			curve += curve * 2.4e-7
			if at(i) > 0 {
				curve = -curve
			}
			for j := start; j < end; j++ {
				set(j, at(j)+curve*at(j)*at(j))
			}

			if special && peakPos >= 2 {
				// The clip started before the first zero crossing, so ramp the
				// correction in from the frame edge rather than stepping.
				offset := x0 - at(0)
				delta := offset / float32(peakPos)
				for j := curr; j < peakPos; j++ {
					offset -= delta
					set(j, min(1, max(-1, at(j)+offset)))
				}
			}

			curr = end
			if curr == n {
				break
			}
		}
		mem[channel] = curve
	}
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}

	return v
}
