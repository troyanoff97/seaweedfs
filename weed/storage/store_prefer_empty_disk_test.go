package storage

import (
	"sync"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
)

// Concurrent AllocateVolume used to race on location pick and stack several
// volumes onto the first empty dir. Prefer-empty + volumeCreateMu must keep
// at most one local volume per dir until every free dir has one.
func TestAddVolumeConcurrentPrefersEmptyDisks(t *testing.T) {
	const dirs = 8
	store := newTestStore(t, dirs)

	var wg sync.WaitGroup
	errs := make(chan error, dirs)
	for i := 1; i <= dirs; i++ {
		wg.Add(1)
		go func(vid needle.VolumeId) {
			defer wg.Done()
			err := store.AddVolume(vid, "", NeedleMapInMemory, "000", "",
				0, needle.GetCurrentVersion(), 0, types.HardDriveType, 3)
			if err != nil {
				errs <- err
			}
		}(needle.VolumeId(i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("AddVolume: %v", err)
	}

	for i, loc := range store.Locations {
		if got := loc.LocalVolumesLen(); got != 1 {
			t.Fatalf("location %d: expected 1 local volume after concurrent empty fill, got %d", i, got)
		}
	}
}
