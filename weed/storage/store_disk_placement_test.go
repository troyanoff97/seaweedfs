package storage

import (
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
)

func TestParseVolumeDiskPlacement(t *testing.T) {
	p, err := ParseVolumeDiskPlacement("roundRobin")
	if err != nil || p != VolumeDiskPlacementRoundRobin {
		t.Fatalf("roundRobin: got %v %v", p, err)
	}
	p, err = ParseVolumeDiskPlacement("leastLoad")
	if err != nil || p != VolumeDiskPlacementLeastLoad {
		t.Fatalf("leastLoad: got %v %v", p, err)
	}
	if _, err := ParseVolumeDiskPlacement("nope"); err == nil {
		t.Fatal("expected error for unknown placement")
	}
}

// With uneven existing counts, leastLoad piles onto the behind disk; roundRobin
// spreads across free dirs instead.
func TestRoundRobinSpreadsDespiteUnevenCounts(t *testing.T) {
	store := newTestStore(t, 3)
	store.SetVolumeDiskPlacement(VolumeDiskPlacementRoundRobin)

	vol := createTestVolume(1001, false)
	store.Locations[0].SetVolume(vol.Id, vol)
	for i := 0; i < 5; i++ {
		vid := needle.VolumeId(2000 + i)
		store.Locations[1].SetVolume(vid, createTestVolume(vid, false))
	}
	for i := 0; i < 5; i++ {
		vid := needle.VolumeId(3000 + i)
		store.Locations[2].SetVolume(vid, createTestVolume(vid, false))
	}

	hits := make([]int, 3)
	for i := 1; i <= 6; i++ {
		vid := needle.VolumeId(i)
		if err := store.AddVolume(vid, "", NeedleMapInMemory, "000", "",
			0, needle.GetCurrentVersion(), 0, types.HardDriveType, 3); err != nil {
			t.Fatalf("AddVolume %d: %v", vid, err)
		}
		for locIdx, loc := range store.Locations {
			if _, found := loc.FindVolume(vid); found {
				hits[locIdx]++
				break
			}
		}
	}
	for i, h := range hits {
		if h != 2 {
			t.Fatalf("roundRobin expected 2 volumes per dir, loc %d got %d (hits=%v)", i, h, hits)
		}
	}
}
