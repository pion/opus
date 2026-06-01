// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package slicetools provides shared helpers for reusing scratch slices.
package slicetools

// Resize returns a slice of size elements, reusing buffer's allocation when
// possible.
func Resize[T any](buffer *[]T, size int) []T {
	if cap(*buffer) < size {
		*buffer = make([]T, size)
	}

	return (*buffer)[:size]
}

// ResizeZero returns a zeroed slice of size elements, reusing buffer's
// allocation when possible.
func ResizeZero[T any](buffer *[]T, size int) []T {
	out := Resize(buffer, size)
	clear(out)

	return out
}
