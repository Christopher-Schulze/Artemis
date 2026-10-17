//go:build linux

package network

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/Christopher-Schulze/Artemis/platform"
	"golang.org/x/sys/unix"
)

func activateTCPFastOpen(socket syscall.RawConn) (platform.ProbeResult, error) {
	result := platform.ProbeResult{State: platform.StateDegraded, Source: "TCP_FASTOPEN_CONNECT actual admitted socket"}
	var optionErr error
	var restoreErr error
	err := socket.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT, 1)
		if optionErr != nil {
			return
		}
		var value int
		value, optionErr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT)
		if optionErr != nil {
			restoreErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT, 0)
		}
		if optionErr == nil && value == 1 {
			result.State, result.Value = platform.StateAvailable, 1
		}
	})
	if err != nil {
		result.Reason = err.Error()
		return result, fmt.Errorf("Fast Open owned socket control: %w", err)
	} else if restoreErr != nil {
		result.Reason = errors.Join(optionErr, restoreErr).Error()
		return result, fmt.Errorf("Fast Open fallback could not disable option: %w", errors.Join(optionErr, restoreErr))
	} else if optionErr != nil {
		result.Reason = optionErr.Error()
	} else if result.State == platform.StateAvailable {
		result.Reason = "owned policy-admitted socket option enabled and read back; peer acceptance and latency savings unmeasured"
	} else {
		result.Reason = "kernel did not retain TCP_FASTOPEN_CONNECT; normal connect remains in use"
	}
	return result, nil
}
