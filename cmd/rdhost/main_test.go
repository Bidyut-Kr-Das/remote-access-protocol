package main

import "testing"

func TestRunRequiresPassword(t *testing.T) {
	if got := run([]string{"-port", "1"}); got != 2 {
		t.Fatalf("run without -password = %d, want 2", got)
	}
}
