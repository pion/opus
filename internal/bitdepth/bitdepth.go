package bitdepth

import (
	"math"
)

func ConvertFloat32LittleEndianToSigned16LittleEndian(in []float32, out []byte, resampleCount int) error {
	currIndex := 0
	for i := range in {
		res := int16(math.Floor(float64(in[i] * 32767)))

		for j := resampleCount; j > 0; j-- {
			out[currIndex] = byte(res & 0b11111111)
			currIndex++

			out[currIndex] = (byte(res >> 8))
			currIndex++
		}
	}

	return nil
}
