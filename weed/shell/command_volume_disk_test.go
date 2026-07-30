package shell

import (
	"bytes"
	"strings"
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/pb"
)

func TestVolumeDiskAdminURL(t *testing.T) {
	got := volumeDiskAdminURL(pb.ServerAddress("10.0.12.21:8088"), "/admin/disk/list")
	want := "http://10.0.12.21:8088/admin/disk/list"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// grpc suffix form host:port.grpcPort
	got = volumeDiskAdminURL(pb.ServerAddress("10.0.12.21:8088.18088"), "/admin/disk/add")
	want = "http://10.0.12.21:8088/admin/disk/add"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWritePrettyJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writePrettyJSON(&buf, []byte(`{"status":"ok","dirs":["/mnt/stor1"]}`)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"status": "ok"`) || !strings.Contains(out, `/mnt/stor1`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestVolumeDiskCommandsRegistered(t *testing.T) {
	want := map[string]bool{
		"volume.disk.list":   false,
		"volume.disk.add":    false,
		"volume.disk.remove": false,
	}
	for _, c := range Commands {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("command %s not registered", name)
		}
	}
}
