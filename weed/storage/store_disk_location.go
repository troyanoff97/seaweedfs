package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/seaweedfs/seaweedfs/weed/glog"
	"github.com/seaweedfs/seaweedfs/weed/stats"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func normalizeDiskDir(dir string) string {
	return filepath.Clean(util.ResolvePath(dir))
}

func (s *Store) SetDiskConfigPath(path string) {
	s.locationsMu.Lock()
	defer s.locationsMu.Unlock()
	s.diskConfigPath = path
}

func (s *Store) DiskConfigPath() string {
	s.locationsMu.RLock()
	defer s.locationsMu.RUnlock()
	return s.diskConfigPath
}

func (s *Store) findLocationIndexLocked(dir string) int {
	want := normalizeDiskDir(dir)
	for i, loc := range s.Locations {
		if normalizeDiskDir(loc.Directory) == want {
			return i
		}
	}
	return -1
}

// AddDiskLocation hot-adds a writable directory to a running volume server.
// Existing volume files under dir are loaded; master is notified via heartbeat.
// The effective disk set is persisted so it survives process restarts.
func (s *Store) AddDiskLocation(dir string, maxVolumeCount int32, minFreeSpace util.MinFreeSpace, diskType types.DiskType, ldbTimeout int64) error {
	if dir == "" {
		return fmt.Errorf("dir is required")
	}
	dir = normalizeDiskDir(dir)
	if maxVolumeCount < 0 {
		return fmt.Errorf("maxVolumeCount must be >= 0")
	}

	if err := util.TestFolderWritable(dir); err != nil {
		return fmt.Errorf("disk location %s not writable: %w", dir, err)
	}

	s.locationsMu.Lock()
	defer s.locationsMu.Unlock()

	if s.findLocationIndexLocked(dir) >= 0 {
		return fmt.Errorf("disk location %s already registered", dir)
	}

	location, err := NewDiskLocationOrError(dir, maxVolumeCount, minFreeSpace, "", diskType)
	if err != nil {
		return err
	}
	location.SetOnDiskHealthChange(func() {
		select {
		case s.DiskHealthChangeChan <- struct{}{}:
		default:
		}
	})

	location.loadExistingVolumes(s.NeedleMapKind, ldbTimeout)

	activeVolumeIDs := make(map[needle.VolumeId]string)
	activeEcVolumeIDs := make(map[needle.VolumeId]string)
	for _, active := range s.Locations {
		for _, id := range active.volumeIds() {
			activeVolumeIDs[id] = active.Directory
		}
		active.ecVolumesLock.RLock()
		for id := range active.ecVolumes {
			activeEcVolumeIDs[id] = active.Directory
		}
		active.ecVolumesLock.RUnlock()
	}
	for _, id := range location.volumeIds() {
		if existingDir, found := activeVolumeIDs[id]; found {
			location.Close()
			return fmt.Errorf("volume %d already loaded from %s", id, existingDir)
		}
	}
	location.ecVolumesLock.RLock()
	for id := range location.ecVolumes {
		if existingDir, found := activeEcVolumeIDs[id]; found {
			location.ecVolumesLock.RUnlock()
			location.Close()
			return fmt.Errorf("ec volume %d already loaded from %s", id, existingDir)
		}
	}
	location.ecVolumesLock.RUnlock()

	// Copy-on-write append so concurrent readers keep a consistent slice.
	newLocs := make([]*DiskLocation, len(s.Locations)+1)
	copy(newLocs, s.Locations)
	newLocs[len(s.Locations)] = location
	s.Locations = newLocs

	stats.VolumeServerMaxVolumeCounter.Add(float64(maxVolumeCount))
	glog.V(0).Infof("hot-added disk location %s max=%d volumes=%d",
		dir, maxVolumeCount, location.VolumesLen())

	if err := s.persistDiskLocationsLocked(); err != nil {
		s.Locations = s.Locations[:len(s.Locations)-1]
		location.Close()
		stats.VolumeServerMaxVolumeCounter.Add(-float64(maxVolumeCount))
		return fmt.Errorf("persist disk config after add: %w", err)
	}
	s.notifyDiskLocationsChanged()
	return nil
}

// RemoveDiskLocation hot-removes a directory from a running volume server.
// If the directory still has volumes/EC shards, force=false refuses removal.
// force=true unloads volumes from memory (does not delete data files) and removes the location.
// The effective disk set is persisted so removals survive process restarts.
func (s *Store) RemoveDiskLocation(dir string, force bool) error {
	if dir == "" {
		return fmt.Errorf("dir is required")
	}
	dir = normalizeDiskDir(dir)

	s.locationsMu.Lock()
	defer s.locationsMu.Unlock()

	idx := s.findLocationIndexLocked(dir)
	if idx < 0 {
		return fmt.Errorf("disk location %s not found", dir)
	}
	if len(s.Locations) == 1 {
		return fmt.Errorf("cannot remove the last disk location %s; add a replacement first", dir)
	}
	location := s.Locations[idx]

	volCount := location.VolumesLen()
	location.ecVolumesLock.RLock()
	ecCount := len(location.ecVolumes)
	location.ecVolumesLock.RUnlock()

	if (volCount > 0 || ecCount > 0) && !force {
		ids := location.volumeIds()
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("%d", id)
		}
		return fmt.Errorf("disk location %s still has %d volume(s) [%s] and %d ec volume(s); use force=true after evacuating/replacing a failed disk",
			dir, volCount, strings.Join(parts, ","), ecCount)
	}

	prevLocs := s.Locations
	location.deactivate()
	newLocs := make([]*DiskLocation, 0, len(s.Locations)-1)
	newLocs = append(newLocs, s.Locations[:idx]...)
	newLocs = append(newLocs, s.Locations[idx+1:]...)
	s.Locations = newLocs

	stats.VolumeServerMaxVolumeCounter.Add(-float64(location.MaxVolumeCount))
	stats.VolumeServerDiskHealthyGauge.DeleteLabelValues(location.Directory)

	if err := s.persistDiskLocationsLocked(); err != nil {
		s.Locations = prevLocs
		location.active.Store(true)
		stats.VolumeServerMaxVolumeCounter.Add(float64(location.MaxVolumeCount))
		return fmt.Errorf("persist disk config after remove: %w", err)
	}

	location.Close()
	glog.V(0).Infof("hot-removed disk location %s force=%v previousVolumes=%d ec=%d", dir, force, volCount, ecCount)

	s.notifyDiskLocationsChanged()
	return nil
}

func (s *Store) persistDiskLocationsLocked() error {
	if s.diskConfigPath == "" {
		return nil
	}
	cfg := &DiskLocationsConfig{
		Version: diskLocationsConfigVersion,
		Disks:   make([]DiskLocationConfigEntry, 0, len(s.Locations)),
	}
	for _, loc := range s.Locations {
		minFree := loc.MinFreeSpace.Raw
		if minFree == "" {
			minFree = loc.MinFreeSpace.String()
		}
		cfg.Disks = append(cfg.Disks, DiskLocationConfigEntry{
			Dir:          loc.Directory,
			Max:          loc.OriginalMaxVolumeCount,
			MinFreeSpace: minFree,
			Disk:         string(loc.DiskType),
		})
	}
	if err := SaveDiskLocationsConfig(s.diskConfigPath, cfg); err != nil {
		return err
	}
	glog.V(0).Infof("persisted %d disk location(s) to %s", len(cfg.Disks), s.diskConfigPath)
	return nil
}

func (s *Store) notifyDiskLocationsChanged() {
	if s.DiskHealthChangeChan == nil {
		return
	}
	select {
	case s.DiskHealthChangeChan <- struct{}{}:
	default:
	}
}

// ListDiskLocations returns directory paths currently registered on this store.
func (s *Store) ListDiskLocations() []string {
	s.locationsMu.RLock()
	defer s.locationsMu.RUnlock()
	out := make([]string, 0, len(s.Locations))
	for _, loc := range s.Locations {
		out = append(out, loc.Directory)
	}
	return out
}
