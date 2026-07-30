package weed_server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/storage"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/util"
)

func TestAdminDiskAddListRemove(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	minFree := util.MinFreeSpace{Type: util.AsPercent, Percent: 1, Raw: "1"}
	store := storage.NewStore(nil, "127.0.0.1", 8088, 0, "", []string{dir1},
		[]int32{8}, []util.MinFreeSpace{minFree}, "", storage.NeedleMapInMemory,
		[]types.DiskType{types.HardDriveType}, 0)
	store.SetDiskConfigPath(filepath.Join(t.TempDir(), "disks.json"))
	defer store.Close()
	vs := &VolumeServer{store: store}

	form := url.Values{
		"dir":          {dir2},
		"max":          {"4"},
		"minFreeSpace": {"1"},
	}
	addReq := httptest.NewRequest(http.MethodPost, "/admin/disk/add", strings.NewReader(form.Encode()))
	addReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRec := httptest.NewRecorder()
	vs.adminDiskAddHandler(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", addRec.Code, addRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	vs.adminDiskListHandler(listRec, httptest.NewRequest(http.MethodGet, "/admin/disk/list", nil))
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), dir2) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	removeForm := url.Values{"dir": {dir2}}
	removeReq := httptest.NewRequest(http.MethodPost, "/admin/disk/remove", strings.NewReader(removeForm.Encode()))
	removeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	removeRec := httptest.NewRecorder()
	vs.adminDiskRemoveHandler(removeRec, removeReq)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removeRec.Code, removeRec.Body.String())
	}
}
