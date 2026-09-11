package storage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/seaweedfs/seaweedfs/weed/glog"
	"github.com/seaweedfs/seaweedfs/weed/stats"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

type diskHealthState int

const (
	diskHealthHealthy diskHealthState = iota
	diskHealthUnhealthy
)

// IsHealthyForWrites returns whether this directory accepts new volume growth and writes.
func (l *DiskLocation) IsHealthyForWrites() bool {
	if !l.IsActive() {
		return false
	}
	l.healthLock.RLock()
	defer l.healthLock.RUnlock()
	return l.health == diskHealthHealthy && !l.isDiskSpaceLow.Load()
}

func (l *DiskLocation) ReportDiskError(err error) {
	if !IsDiskError(err) {
		return
	}
	l.markUnhealthy(err, "io")
}

func (l *DiskLocation) SetInitialHealthFromStartup(err error) {
	if err == nil {
		return
	}
	l.markUnhealthy(err, "startup")
}

func (l *DiskLocation) markUnhealthy(err error, source string) {
	if err == nil {
		return
	}
	l.healthLock.Lock()
	wasHealthy := l.health == diskHealthHealthy
	if wasHealthy {
		l.health = diskHealthUnhealthy
		l.lastHealthError = err
		l.unhealthySince = time.Now()
	}
	l.healthLock.Unlock()

	if wasHealthy {
		volumeIds := l.volumeIds()
		glog.Errorf("disk location %s marked unhealthy (%s): %v; new writes disabled on this directory; existing volumes marked readonly: %s",
			l.Directory, source, err, formatVolumeIds(volumeIds))
		l.publishDiskHealthMetrics()
		l.notifyDiskHealthChange()
		return
	}
	l.publishDiskHealthMetrics()
}

func (l *DiskLocation) tryRecoverHealth() {
	if err := util.TestFolderWritable(l.Directory); err != nil {
		// Proactively detect permission, read-only filesystem, full disk, and
		// synchronous I/O failures without waiting for an application write.
		l.markUnhealthy(err, "probe")
		return
	}

	l.healthLock.Lock()
	wasUnhealthy := l.health == diskHealthUnhealthy
	if wasUnhealthy {
		l.health = diskHealthHealthy
		l.lastHealthError = nil
		l.unhealthySince = time.Time{}
	}
	l.healthLock.Unlock()

	if wasUnhealthy {
		volumeIds := l.volumeIds()
		glog.Infof("disk location %s recovered and is healthy again; volumes restored to writable: %s",
			l.Directory, formatVolumeIds(volumeIds))
		l.publishDiskHealthMetrics()
		l.notifyDiskHealthChange()
		return
	}
	l.publishDiskHealthMetrics()
}

func (l *DiskLocation) checkHealthAndDiskSpace() {
	l.CheckDiskSpace(l.diskProbeConfig)
	l.tryRecoverHealth()
	l.publishDiskHealthMetrics()
}

func (l *DiskLocation) publishDiskHealthMetrics() {
	val := 0.0
	if l.IsHealthyForWrites() {
		val = 1.0
	}
	stats.VolumeServerDiskHealthyGauge.WithLabelValues(l.Directory).Set(val)
}

// DiskHealthSnapshot is used by tests and admin visibility.
type DiskHealthSnapshot struct {
	Directory         string
	Healthy           bool
	DiskSpaceLow      bool
	LastError         error
	UnhealthySince    time.Time
	ReadOnlyVolumeIds []uint32
}

func (l *DiskLocation) volumeIds() []needle.VolumeId {
	l.volumesLock.RLock()
	defer l.volumesLock.RUnlock()
	ids := make([]needle.VolumeId, 0, len(l.volumes))
	for id := range l.volumes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func formatVolumeIds(ids []needle.VolumeId) string {
	if len(ids) == 0 {
		return "none"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, ", ")
}

func (l *DiskLocation) notifyDiskHealthChange() {
	l.onDiskHealthMu.RLock()
	fn := l.onDiskHealthChange
	l.onDiskHealthMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (l *DiskLocation) HealthSnapshot() DiskHealthSnapshot {
	l.healthLock.RLock()
	snap := DiskHealthSnapshot{
		Directory:      l.Directory,
		Healthy:        l.health == diskHealthHealthy,
		DiskSpaceLow:   l.isDiskSpaceLow.Load(),
		LastError:      l.lastHealthError,
		UnhealthySince: l.unhealthySince,
	}
	l.healthLock.RUnlock()

	if !l.IsHealthyForWrites() {
		for _, id := range l.volumeIds() {
			snap.ReadOnlyVolumeIds = append(snap.ReadOnlyVolumeIds, uint32(id))
		}
	}
	return snap
}
