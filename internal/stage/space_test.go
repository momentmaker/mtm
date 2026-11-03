package stage

import (
	"errors"
	"strings"
	"testing"
)

func TestFreeReportsSpace(t *testing.T) {
	free, err := Free(t.TempDir())
	if err != nil || free == 0 {
		t.Errorf("Free = %d, %v; want some space", free, err)
	}
}

func freeSpace(n uint64) func(string) (uint64, error) {
	return func(string) (uint64, error) { return n, nil }
}

func TestRoomKeepsTheReserve(t *testing.T) {
	tests := []struct {
		name       string
		free, need uint64
		ok         bool
	}{
		{"fits beside the reserve", 25_000_000_000, 10_000_000_000, true},
		{"exactly fits", 20_000_000_000, 10_000_000_000, true},
		{"would eat into the reserve", 15_000_000_000, 10_000_000_000, false},
		{"already inside the reserve", 5_000_000_000, 0, false},
	}
	for _, tt := range tests {
		err := Room(freeSpace(tt.free), "/Volumes/X", tt.need)
		if (err == nil) != tt.ok {
			t.Errorf("%s: Room = %v, want ok %v", tt.name, err, tt.ok)
		}
		if err != nil && (!errors.Is(err, ErrNoRoom) || !strings.Contains(err.Error(), "mtm keeps 10.0 GB free")) {
			t.Errorf("%s: err = %v, want ErrNoRoom naming the reserve", tt.name, err)
		}
	}
}

func TestRoomPassesOnAFreeSpaceError(t *testing.T) {
	boom := errors.New("boom")
	if err := Room(func(string) (uint64, error) { return 0, boom }, "/x", 1); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
