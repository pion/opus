// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIlog(t *testing.T) {
	assert.Equal(t, 0, ilog(-1))
	assert.Equal(t, 0, ilog(0))
	assert.Equal(t, 1, ilog(1))
	assert.Equal(t, 2, ilog(2))
	assert.Equal(t, 2, ilog(3))
	assert.Equal(t, 3, ilog(4))
	assert.Equal(t, 3, ilog(7))
}
