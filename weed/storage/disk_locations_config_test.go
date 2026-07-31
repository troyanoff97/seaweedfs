package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/stats"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func TestDiskLocationsConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disks.json")
	cfg := &DiskLocationsConfig{
		Version: 1,
		Disks: []DiskLocationConfigEntry{
			{Dir: "/mnt/stor1", Max: 0, MinFreeSpace: "50GiB"},
			{Dir: "/mnt/stor5", Max: 4, MinFreeSpace: "1", Disk: "hdd"},
		},
	}
	if err := SaveDiskLocationsConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDiskLocationsConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Disks) != 2 || loaded.Disks[1].Dir != normalizeDiskDir("/mnt/stor5") {
		t.Fatalf("loaded=%+v", loaded)
	}
	dirs, maxCounts, minFree, diskTypes, err := loaded.ToStartupArgs()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || maxCounts[1] != 4 || diskTypes[1] != types.ToDiskType("hdd") {
		t.Fatalf("dirs=%v max=%v types=%v", dirs, maxCounts, diskTypes)
	}
	if minFree[0].Raw != "50GiB" {
		t.Fatalf("minFree=%+v", minFree[0])
	}
}

func TestPersistSurvivesRestartSemantics(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "volume.disks.json")
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	dir3 := t.TempDir()

	s := newTestStoreWithDirs(t, dir1, dir2)
	s.SetDiskConfigPath(cfgPath)
	defer s.Close()

	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	if err := s.AddDiskLocation(dir3, 0, minFree, types.HardDriveType, 0); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.RemoveDiskLocation(dir2, true); err != nil {
		t.Fatalf("remove: %v", err)
	}

	loaded, err := LoadDiskLocationsConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Disks) != 2 {
		t.Fatalf("persisted disks=%+v want 2", loaded.Disks)
	}
	got := map[string]bool{}
	for _, d := range loaded.Disks {
		got[d.Dir] = true
	}
	if !got[normalizeDiskDir(dir1)] || !got[normalizeDiskDir(dir3)] || got[normalizeDiskDir(dir2)] {
		t.Fatalf("unexpected persisted set: %+v", loaded.Disks)
	}

	// Simulate restart: CLI still has dir1,dir2 but persisted config wins.
	dirs, maxCounts, minFrees, diskTypes, err := loaded.ToStartupArgs()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(nil, "127.0.0.1", 8088, 0, "", "", dirs, maxCounts, minFrees, "", NeedleMapInMemory, diskTypes, nil, 0, stats.DiskIOProbeConfig{})
	restarted.SetDiskConfigPath(cfgPath)
	defer restarted.Close()
	list := restarted.ListDiskLocations()
	if len(list) != 2 {
		t.Fatalf("restart list=%v", list)
	}
	for _, d := range list {
		if normalizeDiskDir(d) == normalizeDiskDir(dir2) {
			t.Fatalf("removed dir came back after restart: %v", list)
		}
	}
	_ = os.Remove(cfgPath)
}
