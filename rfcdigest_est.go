// Copyright © 2021 Bin Liu <bin.liu@enmotech.com>

package pq

import "testing"

func TestRFC5802Algorithm(t *testing.T) {
	RFC5802Algorithm("1", "2", "3", "4", 5)
}
