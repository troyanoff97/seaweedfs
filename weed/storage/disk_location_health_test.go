package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func TestDiskLocationHealthLifecycle(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "idx")
	if err := os.MkdirAll(idx, 0o755); err != nil {
		t.Fatal(err)
	}

	loc := NewDiskLocation(dir, 8, util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}, idx, types.HardDriveType)
	defer loc.Close()

	if !loc.IsHealthyForWrites() {
		t.Fatal("expected healthy location after startup")
	}

	loc.ReportDiskError(errors.New("read-only file system"))
	if loc.IsHealthyForWrites() {
		t.Fatal("expected unhealthy after disk error")
	}
	snap := loc.HealthSnapshot()
	if snap.Healthy || snap.LastError == nil {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	loc.tryRecoverHealth()
	if !loc.IsHealthyForWrites() {
		t.Fatal("expected recovery on writable directory")
	}
	snap = loc.HealthSnapshot()
	if !snap.Healthy || snap.LastError != nil {
		t.Fatalf("unexpected snapshot after recovery: %+v", snap)
	}
}

func TestFindFreeLocationSkipsUnhealthyDisk(t *testing.T) {
	dirGood := t.TempDir()
	dirBad := t.TempDir()
	idxGood := filepath.Join(dirGood, "idx")
	idxBad := filepath.Join(dirBad, "idx")
	for _, p := range []string{idxGood, idxBad} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	locGood := NewDiskLocation(dirGood, 8, util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}, idxGood, types.HardDriveType)
	locBad := NewDiskLocation(dirBad, 8, util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}, idxBad, types.HardDriveType)
	defer locGood.Close()
	defer locBad.Close()

	locBad.markUnhealthy(errors.New("input/output error"), "test")

	s := &Store{Locations: []*DiskLocation{locGood, locBad}}
	picked := s.FindFreeLocation(nil)
	if picked == nil || picked.Directory != dirGood {
		t.Fatalf("expected good disk, got %v", picked)
	}

	locGood.markUnhealthy(errors.New("input/output error"), "test")
	if s.FindFreeLocation(nil) != nil {
		t.Fatal("expected no writable location when all unhealthy")
	}
}

func TestStartupUnhealthyMountPoint(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "idx")
	if err := os.MkdirAll(idx, 0o755); err != nil {
		t.Fatal(err)
	}

	loc := NewDiskLocation(dir, 4, util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}, idx, types.HardDriveType)
	defer loc.Close()
	loc.SetInitialHealthFromStartup(errors.New("Not writable!"))

	if loc.IsHealthyForWrites() {
		t.Fatal("expected unhealthy when mount point is unavailable at startup")
	}
}
