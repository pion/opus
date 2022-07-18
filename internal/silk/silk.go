package silk

type (
	frameSignalType             byte
	frameQuantizationOffsetType byte
)

const (
	nanoseconds20Ms = 20000000

	frameSignalTypeInactive frameSignalType = iota + 1
	frameSignalTypeUnvoiced
	frameSignalTypeVoiced

	frameQuantizationOffsetTypeLow frameQuantizationOffsetType = iota + 1
	frameQuantizationOffsetTypeHigh
)

func maxUint32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func clamp(low, in, high int32) int32 {
	if in > high {
		return high
	} else if in < low {
		return low
	}

	return in
}
