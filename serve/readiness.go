package serve

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// Readiness binds one successful listener to its concrete server/process owner.
// It contains no authentication credentials.
type Readiness struct {
	Address    string `json:"address"`
	ProcessID  int    `json:"processId"`
	Protocol   string `json:"protocol"`
	InstanceID string `json:"instanceId"`
}

// Validate rejects an unbound endpoint or incomplete ownership receipt.
func (r Readiness) Validate() error {
	if err := validateLoopbackAddress(r.Address); err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(r.Address)
	if err != nil {
		return err
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 || r.ProcessID < 1 || r.Protocol != ProtocolVersion || r.InstanceID == "" || len(r.InstanceID) > 128 {
		return fmt.Errorf("invalid bound endpoint, process, protocol or instance identity")
	}
	return nil
}

func newReadiness(address, instanceID string) Readiness {
	return Readiness{Address: address, ProcessID: os.Getpid(), Protocol: ProtocolVersion, InstanceID: instanceID}
}
