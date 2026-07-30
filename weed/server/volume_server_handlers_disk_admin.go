package weed_server

import (
	"net/http"
	"strconv"

	"github.com/seaweedfs/seaweedfs/weed/glog"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

// POST /admin/disk/add?dir=/mnt/stor5&max=0&minFreeSpace=50GiB&disk=
// Hot-add a disk directory to the running volume server (no process restart).
func (vs *VolumeServer) adminDiskAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		w.Header().Set("Allow", "POST, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	dir := r.FormValue("dir")
	if dir == "" {
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": "dir is required"})
		return
	}
	maxStr := r.FormValue("max")
	if maxStr == "" {
		maxStr = "0"
	}
	maxCount, err := strconv.ParseInt(maxStr, 10, 32)
	if err != nil || maxCount < 0 {
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": "max must be a non-negative integer"})
		return
	}
	minFreeRaw := r.FormValue("minFreeSpace")
	if minFreeRaw == "" {
		minFreeRaw = "1"
	}
	minFree, err := util.ParseMinFreeSpace(minFreeRaw)
	if err != nil {
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": "invalid minFreeSpace: " + err.Error()})
		return
	}
	diskType := types.ToDiskType(r.FormValue("disk"))

	if err := vs.store.AddDiskLocation(dir, int32(maxCount), *minFree, diskType, vs.ldbTimout); err != nil {
		glog.Errorf("admin disk add %s: %v", dir, err)
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	glog.V(0).Infof("admin disk add ok dir=%s max=%d", dir, maxCount)
	writeJsonQuiet(w, r, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"dir":     dir,
		"max":     maxCount,
		"dirs":    vs.store.ListDiskLocations(),
		"message": "disk added; heartbeat will update master",
	})
}

// POST /admin/disk/remove?dir=/mnt/stor4&force=false
// Hot-remove a disk directory. force=true required if volumes still present
// (failed-disk replacement workflow).
func (vs *VolumeServer) adminDiskRemoveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	dir := r.FormValue("dir")
	if dir == "" {
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": "dir is required"})
		return
	}
	force := false
	switch stringsToBool(r.FormValue("force")) {
	case true:
		force = true
	}

	if err := vs.store.RemoveDiskLocation(dir, force); err != nil {
		glog.Errorf("admin disk remove %s force=%v: %v", dir, force, err)
		writeJsonQuiet(w, r, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	glog.V(0).Infof("admin disk remove ok dir=%s force=%v", dir, force)
	writeJsonQuiet(w, r, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"dir":     dir,
		"force":   force,
		"dirs":    vs.store.ListDiskLocations(),
		"message": "disk removed; heartbeat will update master",
	})
}

// GET /admin/disk/list
func (vs *VolumeServer) adminDiskListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJsonQuiet(w, r, http.StatusOK, map[string]interface{}{
		"dirs":       vs.store.ListDiskLocations(),
		"diskHealth": vs.store.DiskHealthStatuses(),
	})
}

func stringsToBool(s string) bool {
	switch s {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes":
		return true
	default:
		return false
	}
}
