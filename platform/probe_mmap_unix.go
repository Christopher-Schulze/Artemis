//go:build darwin || linux

package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func probeMmap() ProbeResult {
	const source = "anonymous read/write mmap and munmap"
	data, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return failedProbe(source, err)
	}
	data[0], data[len(data)-1] = 0x35, 0x79
	valid := data[0] == 0x35 && data[len(data)-1] == 0x79
	if err := unix.Munmap(data); err != nil {
		return failedProbe(source, err)
	}
	if !valid {
		return failedProbe(source, fmt.Errorf("mapped page failed write/read validation"))
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "owned page mapped, written, read and unmapped", Value: 1}
}
