package commands

import (
	"testing"
	"time"
)

func swapTimeNow(t *testing.T, fn func() time.Time) {
	t.Helper()
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	timeNow = fn
}
