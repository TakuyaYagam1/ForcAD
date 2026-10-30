package checker

import (
	"os"
	"testing"
	"time"
)

func TestReaperNotificationMeansOSWaitCompleted(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := startChecker(executable, []string{"check", "192.0.2.1"},
		setEnvironmentValue(os.Environ(), fixtureModeEnv, "sleep"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer process.output.closeReaders()
	t.Cleanup(func() { _ = killProcess(process.command) })
	select {
	case <-process.reaped:
		t.Fatal("process reported reaped while still running")
	default:
	}
	if err := killProcess(process.command); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.reaped:
		select {
		case exit := <-process.waited:
			if !exit.reaped {
				t.Fatalf("exit = %+v", exit)
			}
		default:
			t.Fatal("reap notification preceded the buffered exit classification")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned fixture was not reaped")
	}
}
