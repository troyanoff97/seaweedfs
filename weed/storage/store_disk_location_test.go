package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/pb/master_pb"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func newTestStoreWithDirs(t *testing.T, dirs ...string) *Store {
	t.Helper()
	s := &Store{
		NeedleMapKind:        NeedleMapInMemory,
		NewVolumesChan:       make(chan master_pb.VolumeShortInformationMessage, 16),
		DeletedVolumesChan:   make(chan master_pb.VolumeShortInformationMessage, 16),
		NewEcShardsChan:      make(chan master_pb.VolumeEcShardInformationMessage, 16),
		DeletedEcShardsChan:  make(chan master_pb.VolumeEcShardInformationMessage, 16),
		DiskHealthChangeChan: make(chan struct{}, 1),
	}
	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	for _, dir := range dirs {
		idx := filepath.Join(dir, "idx")
		if err := os.MkdirAll(idx, 0o755); err != nil {
			t.Fatal(err)
		}
		loc := NewDiskLocation(dir, 8, minFree, idx, types.HardDriveType)
		loc.SetOnDiskHealthChange(func() {
			select {
			case s.DiskHealthChangeChan <- struct{}{}:
			default:
			}
		})
		s.Locations = append(s.Locations, loc)
	}
	return s
}

func TestHotAddAndRemoveDiskLocation(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	s := newTestStoreWithDirs(t, dir1)
	defer s.Close()

	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	if err := s.AddDiskLocation(dir2, 4, minFree, types.HardDriveType, 0); err != nil {
		t.Fatalf("AddDiskLocation: %v", err)
	}
	if len(s.ListDiskLocations()) != 2 {
		t.Fatalf("dirs=%v want 2", s.ListDiskLocations())
	}

	// duplicate add must fail
	if err := s.AddDiskLocation(dir2, 4, minFree, types.HardDriveType, 0); err == nil {
		t.Fatal("expected duplicate add error")
	}

	if err := s.RemoveDiskLocation(dir2, false); err != nil {
		t.Fatalf("RemoveDiskLocation empty: %v", err)
	}
	if len(s.ListDiskLocations()) != 1 {
		t.Fatalf("dirs=%v want 1", s.ListDiskLocations())
	}
}

func TestRemoveDiskLocationRefusesWhenVolumesPresent(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	s := newTestStoreWithDirs(t, dir1, dir2)
	defer s.Close()

	if err := s.AddVolume(1, "", NeedleMapInMemory, "000", "", 0, 0, types.HardDriveType, 0); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	// Find which dir got the volume
	var busyDir string
	for _, loc := range s.Locations {
		if loc.VolumesLen() > 0 {
			busyDir = loc.Directory
			break
		}
	}
	if busyDir == "" {
		t.Fatal("expected a volume on one location")
	}

	if err := s.RemoveDiskLocation(busyDir, false); err == nil {
		t.Fatal("expected refuse without force")
	}
	if err := s.RemoveDiskLocation(busyDir, true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if s.HasVolume(1) {
		t.Fatal("volume should be unloaded after force remove")
	}
	for _, d := range s.ListDiskLocations() {
		if d == busyDir {
			t.Fatalf("busy dir still listed: %v", s.ListDiskLocations())
		}
	}
}

func TestAddDiskLocationRequiresWritable(t *testing.T) {
	s := &Store{
		NeedleMapKind:        NeedleMapInMemory,
		DiskHealthChangeChan: make(chan struct{}, 1),
	}
	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := s.AddDiskLocation(missing, 0, minFree, types.HardDriveType, 0); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestRemoveLastDiskLocationRefused(t *testing.T) {
	dir := t.TempDir()
	s := newTestStoreWithDirs(t, dir)
	defer s.Close()

	if err := s.RemoveDiskLocation(dir, true); err == nil {
		t.Fatal("expected error when removing the last disk location")
	}
	if got := s.ListDiskLocations(); len(got) != 1 || got[0] != dir {
		t.Fatalf("last disk changed after rejected remove: %v", got)
	}
}
