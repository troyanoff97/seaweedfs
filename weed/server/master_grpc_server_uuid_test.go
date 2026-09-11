package weed_server

import (
	"testing"

	"github.com/seaweedfs/seaweedfs/weed/pb/master_pb"
	"github.com/seaweedfs/seaweedfs/weed/topology"
)

func TestRegisterUuidsAllowsSameVolumeServerToRefreshLocations(t *testing.T) {
	ms := &MasterServer{Topo: &topology.Topology{}}
	first := &master_pb.Heartbeat{
		Ip:            "10.0.0.1",
		Port:          8088,
		LocationUuids: []string{"disk-a", "disk-b"},
	}
	if _, err := ms.RegisterUuids(first); err != nil {
		t.Fatalf("first register: %v", err)
	}

	updated := &master_pb.Heartbeat{
		Ip:            "10.0.0.1",
		Port:          8088,
		LocationUuids: []string{"disk-a", "disk-c"},
	}
	if _, err := ms.RegisterUuids(updated); err != nil {
		t.Fatalf("same server refresh: %v", err)
	}
	got := ms.Topo.UuidMap["10.0.0.1:8088"]
	if len(got) != 2 || got[0] != "disk-a" || got[1] != "disk-c" {
		t.Fatalf("uuids=%v want [disk-a disk-c]", got)
	}

	empty := &master_pb.Heartbeat{Ip: "10.0.0.1", Port: 8088}
	if _, err := ms.RegisterUuids(empty); err != nil {
		t.Fatalf("empty refresh: %v", err)
	}
	if got := ms.Topo.UuidMap["10.0.0.1:8088"]; len(got) != 0 {
		t.Fatalf("uuids=%v want empty", got)
	}
}

func TestRegisterUuidsRejectsDuplicateOnAnotherVolumeServer(t *testing.T) {
	ms := &MasterServer{Topo: &topology.Topology{}}
	if _, err := ms.RegisterUuids(&master_pb.Heartbeat{
		Ip: "10.0.0.1", Port: 8088, LocationUuids: []string{"disk-a"},
	}); err != nil {
		t.Fatal(err)
	}
	duplicates, err := ms.RegisterUuids(&master_pb.Heartbeat{
		Ip: "10.0.0.2", Port: 8088, LocationUuids: []string{"disk-a"},
	})
	if err == nil || len(duplicates) != 1 || duplicates[0] != "disk-a" {
		t.Fatalf("duplicates=%v err=%v", duplicates, err)
	}
}

func TestRegisterUuidsNoOpWhenUnchanged(t *testing.T) {
	ms := &MasterServer{Topo: &topology.Topology{}}
	hb := &master_pb.Heartbeat{
		Ip: "10.0.0.1", Port: 8088, LocationUuids: []string{"disk-b", "disk-a"},
	}
	if _, err := ms.RegisterUuids(hb); err != nil {
		t.Fatal(err)
	}
	// Same set, different order — must be a no-op (periodic CollectHeartbeat).
	again := &master_pb.Heartbeat{
		Ip: "10.0.0.1", Port: 8088, LocationUuids: []string{"disk-a", "disk-b"},
	}
	if _, err := ms.RegisterUuids(again); err != nil {
		t.Fatal(err)
	}
	got := ms.Topo.UuidMap["10.0.0.1:8088"]
	if len(got) != 2 || got[0] != "disk-b" || got[1] != "disk-a" {
		t.Fatalf("uuids=%v want original order preserved on no-op", got)
	}
}
