package tray

import (
	"runtime"
	"testing"
	"time"
)

func TestTrayRun(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping on non-windows")
	}

	errCh := make(chan error, 1)
	go func() {
		err := Run(Config{
			Title: "Test Gateway",
			Port:  9527,
			OnExit: func() {
				t.Log("OnExit triggered")
			},
		})
		errCh <- err
	}()

	select {
	case err := <-errCh:
		t.Fatalf("Run returned immediately with: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Log("Run is running properly and waiting for messages")
		Stop()
	}
}
