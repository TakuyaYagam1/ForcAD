//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package checker

import (
	"os/signal"
	"syscall"
)

func fixtureIgnoreTermination() {
	signal.Ignore(syscall.SIGTERM)
}
