package data

import (
	"testing"
	"time"
)

func TestSystemClockReturnsCurrentUTC(t *testing.T) {
	clock := NewSystemClock()
	before := time.Now().UTC()
	actual := clock.Now()
	after := time.Now().UTC()
	if actual.Location() != time.UTC {
		t.Fatalf("Now() location = %s, want UTC", actual.Location())
	}
	if actual.Before(before) || actual.After(after) {
		t.Fatalf("Now() = %s, want within %s..%s", actual, before, after)
	}
}
