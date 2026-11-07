//go:build windows

package clipboard

import "testing"

func TestRealClipboardRoundTrip(t *testing.T) {
	prev, _ := get()
	defer set(prev)
	before := seq()
	if err := set("rap ✓"); err != nil {
		t.Fatal(err)
	}
	if got, err := get(); err != nil || got != "rap ✓" {
		t.Fatalf("got %q, %v", got, err)
	}
	if seq() == before {
		t.Fatal("sequence number did not advance")
	}
}
