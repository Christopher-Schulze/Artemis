//go:build !darwin && !linux

package process

import (
	"context"
	"fmt"
)

func processGroupCPUStopped(context.Context, int) (bool, error) {
	return false, fmt.Errorf("this build cannot prove owned process-group CPU exit")
}
