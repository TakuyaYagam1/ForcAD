//go:build !linux

package load_test

import "fmt"

func readAttackLinuxClock() (attackClockAnchor, error) {
	const location = "load.readAttackLinuxClock"
	return attackClockAnchor{}, fmt.Errorf("%s - absolute Linux CLOCK_MONOTONIC anchors are unavailable on this platform", location)
}
