// +build linux

// An implementation of the VmdkCmdRunner interface that mocks ESX. This removes the requirement forunning ESX at all when testing the plugin.

package vmdkops

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/vmware/docker-vmdk-plugin/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// MockVmdkCmd struct
type MockVmdkCmd struct{}

const (
	backingRoot = "/tmp/docker-volumes" // Files for loopback device backing stored here
)

// Run returns JSON responses to each command or an error
func (mockCmd MockVmdkCmd) Run(cmd string, name string, opts map[string]string) ([]byte, error) {
	// We store no in memory state, so just try to recreate backingRoot every time
	err := fs.Mkdir(backingRoot)
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{"cmd": cmd}).Debug("Running Mock Cmd")
	switch cmd {
	case "create":
		err := createBlockDevice(name)
		return nil, err
	case "list":
		return list()
	case "attach":
		return nil, nil
	case "detach":
		return nil, nil
	case "remove":
		err := remove(name)
		return nil, err
	}
	return []byte("null"), nil
}

func list() ([]byte, error) {
	files, err := ioutil.ReadDir(backingRoot)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s", backingRoot)
	}
	volumes := make([]VolumeData, 0, len(files))
	for _, file := range files {
		volumes = append(volumes, VolumeData{Name: file.Name()})
	}
	return json.Marshal(volumes)
}

func remove(name string) error {
	backing := fmt.Sprintf("%s/%s", backingRoot, name)
	out, err := exec.Command("blkid", []string{"-L", name}...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to find device for backing file %s via blkid", backing)
	}
	device := strings.TrimRight(string(out), " \n")
	fmt.Printf("Detaching loopback device %s\n", device)
	out, err = exec.Command("losetup", "-d", device).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to detach loopback device node %s with error: %s. Output = %s",
			device, err, out)
	}
	err = os.Remove(backing)
	if err != nil {
		return fmt.Errorf("Failed to remove backing file %s: %s", backing, err)
	}
	return os.Remove(device)
}

func createBlockDevice(label string) error {
	backing := fmt.Sprintf("%s/%s", backingRoot, label)
	err := createBackingFile(backing)
	if err != nil {
		return err
	}
	loopbackCount := getMaxLoopbackCount() + 1
	device := fmt.Sprintf("/dev/loop%d", loopbackCount)
	err = createDeviceNode(device, loopbackCount)
	if err != nil {
		return err
	}
	// Ignore output. This is to prevent spurious failures from old devices
	// that were removed, but not detached.
	exec.Command("losetup", "-d", device).CombinedOutput()
	err = setupLoopbackDevice(backing, device)
	if err != nil {
		return err
	}
	return makeFilesystem(device, label)
}

func getMaxLoopbackCount() int {
	// always start at 1000
	count := 1000
	files, err := ioutil.ReadDir("/dev")
	if err != nil {
		panic("Failed to read /dev")
	}
	for _, file := range files {
		trimmed := strings.TrimPrefix(file.Name(), "loop")
		if s, err := strconv.Atoi(trimmed); err == nil {
			if s > count {
				count = s
			}
		}
	}
	return count
}

func createBackingFile(backing string) error {
	flags := syscall.O_RDWR | syscall.O_CREAT | syscall.O_EXCL
	file, err := os.OpenFile(backing, flags, 0755)
	if err != nil {
		return fmt.Errorf("Failed to create backing file %s: %s.", backing, err)
	}
	err = syscall.Fallocate(int(file.Fd()), 0, 0, 100*1024*1024)
	if err != nil {
		return fmt.Errorf("Failed to allocate %s with error: %s.", backing, err)
	}
	return nil
}

func createDeviceNode(device string, loopbackCount int) error {
	count := fmt.Sprintf("%d", loopbackCount)
	out, err := exec.Command("mknod", device, "b", "7", count).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to make device node %s with error: %s. Output = %s",
			device, err, out)
	}
	return nil
}

func setupLoopbackDevice(backing string, device string) error {
	out, err := exec.Command("losetup", device, backing).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to setup loopback device node %s for backing file %s with error: %s. Output = %s",
			device, backing, err, out)
	}
	return nil
}

func makeFilesystem(device string, label string) error {
	out, err := exec.Command("mkfs.ext4", "-L", label, device).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to create filesystem on %s with error: %s. Output = %s",
			device, err, out)
	}
	return nil
}
