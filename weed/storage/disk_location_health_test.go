package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/pb/master_pb"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func newHealthTestLocation(dir string) *DiskLocation {
	loc := &DiskLocation{
		Directory:    dir,
		MinFreeSpace: util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"},
		volumes:      make(map[needle.VolumeId]*Volume),
		health:       diskHealthHealthy,
		closeCh:      make(chan struct{}),
	}
	loc.active.Store(true)
	return loc
}

func TestDiskLocationHealthLifecycle(t *testing.T) {
	dir := t.TempDir()
	loc := newHealthTestLocation(dir)
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

func TestWritableProbeMarksLocationUnhealthyAndRecovers(t *testing.T) {
	dir := t.TempDir()
	loc := newHealthTestLocation(dir)
	defer loc.Close()

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	loc.tryRecoverHealth()
	if loc.IsHealthyForWrites() {
		t.Fatal("expected missing disk location to fail the real write probe")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	loc.tryRecoverHealth()
	if !loc.IsHealthyForWrites() {
		t.Fatal("expected disk location to recover after write probe succeeds")
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

func TestAddVolumeReportsDiskError(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "idx")
	if err := os.MkdirAll(idx, 0o755); err != nil {
		t.Fatal(err)
	}

	loc := NewDiskLocation(dir, 4, util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}, idx, types.HardDriveType)
	defer loc.Close()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(idx, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(idx, 0o755)
		_ = os.Chmod(dir, 0o755)
	}()

	s := &Store{Locations: []*DiskLocation{loc}}
	err := s.addVolume(1, "", NeedleMapInMemory, nil, nil, 0, 0, types.HardDriveType, 0)
	if err == nil {
		t.Fatal("expected addVolume to fail on readonly directory")
	}
	if loc.IsHealthyForWrites() {
		t.Fatal("expected location marked unhealthy after failed volume growth")
	}
}

func TestUnhealthyDirMarksExistingVolumesReadOnly(t *testing.T) {
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

	s := &Store{
		Locations:      []*DiskLocation{locGood, locBad},
		NewVolumesChan: make(chan master_pb.VolumeShortInformationMessage, 3),
	}
	if err := s.AddVolume(1, "", NeedleMapInMemory, "000", "", 0, 0, types.HardDriveType, 0); err != nil {
		t.Fatal(err)
	}
	vol := s.findVolume(1)
	if vol == nil {
		t.Fatal("expected volume 1")
	}
	if vol.location.Directory != dirGood {
		t.Fatalf("expected volume on %s, got %s", dirGood, vol.location.Directory)
	}
	if vol.IsReadOnly() {
		t.Fatal("expected writable volume before disk failure")
	}

	locGood.markUnhealthy(errors.New("input/output error"), "test")
	if !vol.IsReadOnly() {
		t.Fatal("expected existing volume readonly when dir is unhealthy")
	}
	snap := locGood.HealthSnapshot()
	if len(snap.ReadOnlyVolumeIds) != 1 || snap.ReadOnlyVolumeIds[0] != 1 {
		t.Fatalf("unexpected readonly volume ids in snapshot: %+v", snap.ReadOnlyVolumeIds)
	}

	locGood.tryRecoverHealth()
	if vol.IsReadOnly() {
		t.Fatal("expected volume writable after dir recovery")
	}
}

func TestHeartbeatReportsUnhealthyDirVolumesReadOnly(t *testing.T) {
	dirData1 := t.TempDir()
	dirData2 := t.TempDir()
	idx1 := filepath.Join(dirData1, "idx")
	idx2 := filepath.Join(dirData2, "idx")
	for _, p := range []string{idx1, idx2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	locData1 := NewDiskLocation(dirData1, 8, minFree, idx1, types.HardDriveType)
	locData2 := NewDiskLocation(dirData2, 8, minFree, idx2, types.HardDriveType)
	defer locData1.Close()
	defer locData2.Close()

	s := &Store{
		Ip:             "127.0.0.1",
		Port:           8080,
		Locations:      []*DiskLocation{locData1, locData2},
		NewVolumesChan: make(chan master_pb.VolumeShortInformationMessage, 3),
	}
	if err := s.AddVolume(1, "", NeedleMapInMemory, "000", "", 0, 0, types.HardDriveType, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.AddVolume(2, "", NeedleMapInMemory, "000", "", 0, 0, types.HardDriveType, 0); err != nil {
		t.Fatal(err)
	}

	locData1.markUnhealthy(errors.New("input/output error"), "test")

	hb := s.CollectHeartbeat()
	readonly := map[uint32]bool{}
	for _, vm := range hb.Volumes {
		readonly[vm.Id] = vm.ReadOnly
	}
	if !readonly[1] {
		t.Fatal("heartbeat must mark volume on unhealthy dir as readonly")
	}
	if readonly[2] {
		t.Fatal("heartbeat must keep volume on healthy dir writable")
	}

	_, writeErr := s.WriteVolumeNeedle(1, new(needle.Needle), false, false)
	if writeErr == nil {
		t.Fatal("PUT path must reject write to readonly volume on unhealthy dir")
	}
}
