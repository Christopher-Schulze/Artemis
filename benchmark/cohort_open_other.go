//go:build !darwin && !linux

package benchmark

import (
	"fmt"
	"os"
)

func openCohortMember(root *os.Root, name string) (*os.File, error) {
	return nil, fmt.Errorf("artifact: secure file admission is supported on macOS and Linux")
}
