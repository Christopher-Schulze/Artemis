//go:build !linux

package network

import (
	"syscall"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func activateTCPFastOpen(syscall.RawConn) (platform.ProbeResult, error) {
	return platform.ProbeResult{State: platform.StateUnavailable, Source: "TCP_FASTOPEN_CONNECT backend", Reason: "no socket activation backend linked on this platform"}, nil
}
