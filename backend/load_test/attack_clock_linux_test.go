//go:build linux

package load_test

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readAttackLinuxClock() (attackClockAnchor, error) {
	const location = "load.readAttackLinuxClock"
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return attackClockAnchor{}, fmt.Errorf("%s - read kernel boot ID: %w", location, err)
	}
	timeNamespace, err := os.Readlink("/proc/self/ns/time")
	if err != nil {
		return attackClockAnchor{}, fmt.Errorf("%s - read time namespace: %w", location, err)
	}
	var clock unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &clock); err != nil {
		return attackClockAnchor{}, fmt.Errorf("%s - read CLOCK_MONOTONIC: %w", location, err)
	}
	return attackClockAnchor{
		monotonicNS:   clock.Nano(),
		bootID:        strings.TrimSpace(string(bootID)),
		timeNamespace: timeNamespace,
	}, nil
}
