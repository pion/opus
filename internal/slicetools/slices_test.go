// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package slicetools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResize(t *testing.T) {
	buffer := []int{1, 2, 3}

	assert.Equal(t, []int{1, 2}, Resize(&buffer, 2))
	assert.Equal(t, []int{0, 0, 0, 0}, Resize(&buffer, 4))
}

func TestResizeZero(t *testing.T) {
	buffer := []int{1, 2, 3}

	assert.Equal(t, []int{0, 0}, ResizeZero(&buffer, 2))
}
