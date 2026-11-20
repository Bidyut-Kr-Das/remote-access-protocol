package main

import "testing"

func TestRunRequiresPassword(t *testing.T) {
	if got := run([]string{"-port", "1"}); got != 2 {
		t.Fatalf("run without -password = %d, want 2", got)
	}
}

func TestRunRejectsBadCodecFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-codec", "h264"},
		{"-codec", ""},
		{"-bitrate", "99"},
		{"-bitrate", "0"},
	} {
		if got := run(append(args, "-password", "x", "-port", "1")); got != 2 {
			t.Errorf("run %v = %d, want 2", args, got)
		}
	}
}
