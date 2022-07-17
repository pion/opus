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

func max(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
