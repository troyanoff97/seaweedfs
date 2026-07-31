package topology

import (
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/pb/master_pb"
	"github.com/seaweedfs/seaweedfs/weed/sequence"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/super_block"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
)

func TestMasterAssignSkipsVolumesOnUnhealthyDiskDir(t *testing.T) {
	topo := NewTopology("weedfs", sequence.NewMemorySequencer(), 32*1024, 5, false)
	dc := topo.GetOrCreateDataCenter("dc1")
	rack := dc.GetOrCreateRack("rack1")
	maxVolumeCounts := map[string]uint32{"": 25}
	dn := rack.GetOrCreateDataNode("127.0.0.1", 8080, 0, "127.0.0.1", "", maxVolumeCounts)

	volumeMessages := []*master_pb.VolumeInformationMessage{
		{
			Id:               1,
			Size:             1024,
			FileCount:        1,
			ReadOnly:         false,
			ReplicaPlacement: 0,
			Version:          uint32(needle.GetCurrentVersion()),
		},
		{
			Id:               2,
			Size:             1024,
			FileCount:        1,
			ReadOnly:         false,
			ReplicaPlacement: 0,
			Version:          uint32(needle.GetCurrentVersion()),
		},
	}
	topo.SyncDataNodeRegistration(volumeMessages, dn)

	rp, err := super_block.NewReplicaPlacementFromString("000")
	if err != nil {
		t.Fatal(err)
	}
	layout := topo.GetVolumeLayout("", rp, needle.EMPTY_TTL, types.HardDriveType)

	for i := 0; i < 10; i++ {
		vid, _, _, _, pickErr := layout.PickForWrite(0, &VolumeGrowOption{})
		if pickErr != nil {
			t.Fatalf("PickForWrite before unhealthy dir: %v", pickErr)
		}
		if vid != 1 && vid != 2 {
			t.Fatalf("expected volume 1 or 2, got %d", vid)
		}
	}

	// Simulate heartbeat after /data1 became unhealthy: volume 1 is readonly.
	volumeMessages[0].ReadOnly = true
	topo.SyncDataNodeRegistration(volumeMessages, dn)

	for i := 0; i < 20; i++ {
		vid, _, _, _, pickErr := layout.PickForWrite(0, &VolumeGrowOption{})
		if pickErr != nil {
			t.Fatalf("PickForWrite after unhealthy dir: %v", pickErr)
		}
		if vid == 1 {
			t.Fatalf("assign must not return volume ID on unhealthy dir, got %d", vid)
		}
		if vid != 2 {
			t.Fatalf("assign must use healthy dir volume 2, got %d", vid)
		}
	}

	// Simulate recovery heartbeat.
	volumeMessages[0].ReadOnly = false
	topo.SyncDataNodeRegistration(volumeMessages, dn)

	for i := 0; i < 10; i++ {
		vid, _, _, _, pickErr := layout.PickForWrite(0, &VolumeGrowOption{})
		if pickErr != nil {
			t.Fatalf("PickForWrite after recovery: %v", pickErr)
		}
		if vid != 1 && vid != 2 {
			t.Fatalf("expected volume 1 or 2 after recovery, got %d", vid)
		}
	}
}
