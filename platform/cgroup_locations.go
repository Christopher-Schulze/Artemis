package platform

import (
	"fmt"
	"path"
	"strings"
)

type cgroupLocation struct {
	mount   string
	leaf    string
	version int
}

func cgroupLocationFor(membership, mounts []byte, controller string) (cgroupLocation, error) {
	group, version, err := cgroupMembership(membership, controller)
	if err != nil {
		return cgroupLocation{}, err
	}
	var selected cgroupLocation
	bestRoot := int(^uint(0) >> 1)
	for _, line := range strings.Split(string(mounts), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		before, after := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(before) < 6 || len(after) < 3 {
			continue
		}
		if (version == 2 && after[0] != "cgroup2") || (version == 1 && (after[0] != "cgroup" || !commaContains(after[2], controller))) {
			continue
		}
		root, err := cgroupPath(before[3])
		if err != nil {
			return cgroupLocation{}, err
		}
		mount, err := cgroupPath(before[4])
		if err != nil {
			return cgroupLocation{}, err
		}
		if root != "/" && group != root && !strings.HasPrefix(group, root+"/") {
			continue
		}
		if len(root) >= bestRoot {
			continue
		}
		selected = cgroupLocation{mount: mount, leaf: path.Join(mount, strings.TrimPrefix(group, root)), version: version}
		bestRoot = len(root)
	}
	if selected.mount == "" {
		return cgroupLocation{}, fmt.Errorf("cgroup %s mount not observable", controller)
	}
	return selected, nil
}

func cgroupMembership(data []byte, controller string) (string, int, error) {
	var unified string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}
		if fields[0] == "0" && fields[1] == "" {
			unified = fields[2]
		}
		if commaContains(fields[1], controller) {
			group, err := cgroupPath(fields[2])
			return group, 1, err
		}
	}
	if unified != "" {
		group, err := cgroupPath(unified)
		return group, 2, err
	}
	return "", 0, fmt.Errorf("process %s cgroup membership not observable", controller)
}

func commaContains(list, value string) bool {
	for _, item := range strings.Split(list, ",") {
		if item == value {
			return true
		}
	}
	return false
}

func cgroupPath(encoded string) (string, error) {
	decoded := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, "\\").Replace(encoded)
	if !strings.HasPrefix(decoded, "/") || strings.ContainsAny(decoded, "\\\x00\n\r") || path.Clean(decoded) != decoded {
		return "", fmt.Errorf("invalid or unreachable cgroup path")
	}
	return decoded, nil
}
