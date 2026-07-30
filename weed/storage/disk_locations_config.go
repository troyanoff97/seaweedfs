package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

const diskLocationsConfigVersion = 1

// DiskLocationConfigEntry is one persisted disk directory entry.
type DiskLocationConfigEntry struct {
	Dir          string `json:"dir"`
	Max          int32  `json:"max"`
	MinFreeSpace string `json:"minFreeSpace"`
	Disk         string `json:"disk,omitempty"`
}

// DiskLocationsConfig is the runtime disk set written by /admin/disk/add|remove.
// When present at startup it overrides CLI -dir/-max/-minFreeSpace/-disk.
type DiskLocationsConfig struct {
	Version int                       `json:"version"`
	Disks   []DiskLocationConfigEntry `json:"disks"`
}

func DefaultDiskLocationsConfigPath(ip string, port int) string {
	safeIP := strings.ReplaceAll(ip, ":", "_")
	return filepath.Join("/var/lib/seaweedfs", fmt.Sprintf("volume.%s.%d.disks.json", safeIP, port))
}

func LoadDiskLocationsConfig(path string) (*DiskLocationsConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg DiskLocationsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse disk config %s: %w", path, err)
	}
	if cfg.Version == 0 {
		cfg.Version = diskLocationsConfigVersion
	}
	for i := range cfg.Disks {
		cfg.Disks[i].Dir = normalizeDiskDir(cfg.Disks[i].Dir)
		if cfg.Disks[i].MinFreeSpace == "" {
			cfg.Disks[i].MinFreeSpace = "1"
		}
	}
	return &cfg, nil
}

func SaveDiskLocationsConfig(path string, cfg *DiskLocationsConfig) error {
	if path == "" {
		return fmt.Errorf("disk config path is empty")
	}
	if cfg == nil {
		return fmt.Errorf("disk config is nil")
	}
	cfg.Version = diskLocationsConfigVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for disk config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write disk config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename disk config: %w", err)
	}
	return nil
}

func (cfg *DiskLocationsConfig) ToStartupArgs() (dirs []string, maxCounts []int32, minFreeSpaces []util.MinFreeSpace, diskTypes []types.DiskType, err error) {
	if cfg == nil || len(cfg.Disks) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("disk config has no disks")
	}
	for _, d := range cfg.Disks {
		if d.Dir == "" {
			return nil, nil, nil, nil, fmt.Errorf("disk config entry missing dir")
		}
		minFree, parseErr := util.ParseMinFreeSpace(d.MinFreeSpace)
		if parseErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("disk %s minFreeSpace: %w", d.Dir, parseErr)
		}
		dirs = append(dirs, d.Dir)
		maxCounts = append(maxCounts, d.Max)
		minFreeSpaces = append(minFreeSpaces, *minFree)
		diskTypes = append(diskTypes, types.ToDiskType(d.Disk))
	}
	return dirs, maxCounts, minFreeSpaces, diskTypes, nil
}
