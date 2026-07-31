package shell

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"

	"github.com/seaweedfs/seaweedfs/weed/pb"
	util_http "github.com/seaweedfs/seaweedfs/weed/util/http"
)

func init() {
	Commands = append(Commands, &commandVolumeDiskList{})
	Commands = append(Commands, &commandVolumeDiskAdd{})
	Commands = append(Commands, &commandVolumeDiskRemove{})
}

type commandVolumeDiskList struct{}

func (c *commandVolumeDiskList) Name() string { return "volume.disk.list" }

func (c *commandVolumeDiskList) Help() string {
	return `list disk directories on a volume server

	volume.disk.list -node <host:port>

	Uses the volume HTTP admin API (/admin/disk/list).
	Shows registered dirs, disk health, and persisted -dir.config path.
`
}

func (c *commandVolumeDiskList) HasTag(CommandTag) bool { return false }

func (c *commandVolumeDiskList) Do(args []string, commandEnv *CommandEnv, writer io.Writer) error {
	fs := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	node := fs.String("node", "", "volume server <host>:<port>")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	if *node == "" {
		return fmt.Errorf("need -node=<host>:<port>")
	}
	return volumeDiskAdminGet(pb.ServerAddress(*node), "/admin/disk/list", writer)
}

type commandVolumeDiskAdd struct{}

func (c *commandVolumeDiskAdd) Name() string { return "volume.disk.add" }

func (c *commandVolumeDiskAdd) Help() string {
	return `hot-add a disk directory on a running volume server

	volume.disk.add -node <host:port> -dir=/mnt/stor5 -max=0 -minFreeSpace=50GiB

	Persisted to -dir.config; survives restart without editing systemd -dir.
	Requires "lock" first.
`
}

func (c *commandVolumeDiskAdd) HasTag(CommandTag) bool { return false }

func (c *commandVolumeDiskAdd) Do(args []string, commandEnv *CommandEnv, writer io.Writer) error {
	fs := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	node := fs.String("node", "", "volume server <host>:<port>")
	dir := fs.String("dir", "", "directory to add, e.g. /mnt/stor5")
	max := fs.String("max", "0", "max volumes for this dir (0 = auto)")
	minFree := fs.String("minFreeSpace", "1", "min free space, e.g. 50GiB or 1 (%)")
	disk := fs.String("disk", "", "disk type tag [hdd|ssd|<tag>]")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	if err := commandEnv.confirmIsLocked(args); err != nil {
		return err
	}
	if *node == "" {
		return fmt.Errorf("need -node=<host>:<port>")
	}
	if *dir == "" {
		return fmt.Errorf("need -dir=/path")
	}
	values := url.Values{}
	values.Set("dir", *dir)
	values.Set("max", *max)
	values.Set("minFreeSpace", *minFree)
	if *disk != "" {
		values.Set("disk", *disk)
	}
	return volumeDiskAdminPost(pb.ServerAddress(*node), "/admin/disk/add", values, writer)
}

type commandVolumeDiskRemove struct{}

func (c *commandVolumeDiskRemove) Name() string { return "volume.disk.remove" }

func (c *commandVolumeDiskRemove) Help() string {
	return `hot-remove a disk directory from a running volume server

	volume.disk.remove -node <host:port> -dir=/mnt/stor4
	volume.disk.remove -node <host:port> -dir=/mnt/stor4 -force

	-force is required if volumes are still registered on that dir
	(failed-disk replacement). Persisted; survives restart.
	Requires "lock" first. Cannot remove the last remaining disk.
`
}

func (c *commandVolumeDiskRemove) HasTag(CommandTag) bool { return false }

func (c *commandVolumeDiskRemove) Do(args []string, commandEnv *CommandEnv, writer io.Writer) error {
	fs := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	node := fs.String("node", "", "volume server <host>:<port>")
	dir := fs.String("dir", "", "directory to remove, e.g. /mnt/stor4")
	force := fs.Bool("force", false, "force remove even if volumes are still registered")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	if err := commandEnv.confirmIsLocked(args); err != nil {
		return err
	}
	if *node == "" {
		return fmt.Errorf("need -node=<host>:<port>")
	}
	if *dir == "" {
		return fmt.Errorf("need -dir=/path")
	}
	values := url.Values{}
	values.Set("dir", *dir)
	if *force {
		values.Set("force", "true")
	} else {
		values.Set("force", "false")
	}
	return volumeDiskAdminPost(pb.ServerAddress(*node), "/admin/disk/remove", values, writer)
}

func volumeDiskAdminURL(node pb.ServerAddress, path string) string {
	return "http://" + node.ToHttpAddress() + path
}

func volumeDiskAdminGet(node pb.ServerAddress, path string, writer io.Writer) error {
	body, _, err := util_http.Get(volumeDiskAdminURL(node, path))
	if err != nil {
		return err
	}
	return writePrettyJSON(writer, body)
}

func volumeDiskAdminPost(node pb.ServerAddress, path string, values url.Values, writer io.Writer) error {
	body, err := util_http.Post(volumeDiskAdminURL(node, path), values)
	if err != nil {
		return err
	}
	return writePrettyJSON(writer, body)
}

func writePrettyJSON(writer io.Writer, body []byte) error {
	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		_, werr := writer.Write(append(body, '\n'))
		return werr
	}
	out, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, string(out))
	return err
}
