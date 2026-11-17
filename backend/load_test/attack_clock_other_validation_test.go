//go:build !linux

package load_test

import (
	"strings"
	"testing"
)

func TestAttackLinuxClockUnsupportedPlatformIsExplicit(t *testing.T) {
	anchor, err := readAttackLinuxClock()
	if err == nil || !strings.Contains(err.Error(), "absolute Linux CLOCK_MONOTONIC anchors are unavailable") || anchor != (attackClockAnchor{}) {
		t.Fatalf("unsupported clock source was not explicit: anchor=%+v err=%v", anchor, err)
	}
}
