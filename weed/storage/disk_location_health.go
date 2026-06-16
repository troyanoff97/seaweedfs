package storage

import (
	"time"

	"github.com/seaweedfs/seaweedfs/weed/glog"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

type diskHealthState int

const (
	diskHealthHealthy diskHealthState = iota
	diskHealthUnhealthy
)

// IsHealthyForWrites returns whether this directory accepts new volume growth and writes.
func (l *DiskLocation) IsHealthyForWrites() bool {
	l.healthLock.RLock()
	defer l.healthLock.RUnlock()
	return l.health == diskHealthHealthy && !l.isDiskSpaceLow
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
		glog.Errorf("disk location %s marked unhealthy (%s): %v; new writes disabled on this directory",
			l.Directory, source, err)
	}
}

func (l *DiskLocation) tryRecoverHealth() {
	if err := util.TestFolderWritable(l.Directory); err != nil {
		l.healthLock.RLock()
		wasUnhealthy := l.health == diskHealthUnhealthy
		l.healthLock.RUnlock()
		if wasUnhealthy {
			glog.V(4).Infof("disk location %s still unhealthy: %v", l.Directory, err)
		}
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
		glog.Infof("disk location %s recovered and is healthy again; writes re-enabled", l.Directory)
	}
}

func (l *DiskLocation) checkHealthAndDiskSpace() {
	l.CheckDiskSpace()
	l.tryRecoverHealth()
}

// DiskHealthSnapshot is used by tests and admin visibility.
type DiskHealthSnapshot struct {
	Directory      string
	Healthy        bool
	DiskSpaceLow   bool
	LastError      error
	UnhealthySince time.Time
}

func (l *DiskLocation) HealthSnapshot() DiskHealthSnapshot {
	l.healthLock.RLock()
	defer l.healthLock.RUnlock()
	return DiskHealthSnapshot{
		Directory:      l.Directory,
		Healthy:        l.health == diskHealthHealthy,
		DiskSpaceLow:   l.isDiskSpaceLow,
		LastError:      l.lastHealthError,
		UnhealthySince: l.unhealthySince,
	}
}