// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "testing"

func TestIlog(t *testing.T) {
	if ilog(-1) != 0 {
		t.Fatal()
	}

	if ilog(0) != 0 {
		t.Fatal()
	}

	if ilog(1) != 1 {
		t.Fatal()
	}

	if ilog(2) != 2 {
		t.Fatal()
	}

	if ilog(3) != 2 {
		t.Fatal()
	}

	if ilog(4) != 3 {
		t.Fatal()
	}

	if ilog(7) != 3 {
		t.Fatal()
	}
}
