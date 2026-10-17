//go:build !darwin && !linux

package fixture

import (
	"fmt"
	"os"
)

func openReplayFile(root *os.Root, name string) (*os.File, error) {
	return nil, fmt.Errorf("replay: secure no-follow file admission is supported on macOS and Linux")
}
