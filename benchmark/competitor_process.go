package benchmark

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

type boundedCommandOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedCommandOutput) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.buffer.Len() {
		return 0, fmt.Errorf("competitor command output exceeds %d bytes", w.limit)
	}
	return w.buffer.Write(data)
}

func (r *CompetitorRunner) connect(ctx context.Context, address string) error {
	startup, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	for {
		select {
		case <-r.waitDone:
			return errors.Join(fmt.Errorf("competitor exited before CDP readiness: %v", r.waitErr), r.Stop())
		default:
		}
		probe, cancelProbe := context.WithTimeout(startup, 250*time.Millisecond)
		transport, err := bridge.DialCDPTransport(probe, bridge.CDPTransportConfig{URL: "ws://" + address, MaxMessageBytes: 16 * 1024 * 1024})
		if err == nil {
			var version struct {
				Product string `json:"product"`
			}
			err = transport.Call(probe, "Browser.getVersion", struct{}{}, &version)
			if err == nil && version.Product == "" {
				err = fmt.Errorf("empty CDP browser identity")
			}
		}
		cancelProbe()
		if err == nil {
			select {
			case <-r.waitDone:
				return errors.Join(fmt.Errorf("competitor exited during readiness"), transport.Close(), r.Stop())
			default:
			}
			r.transport = transport
			return nil
		}
		if transport != nil {
			if closeErr := transport.Close(); closeErr != nil {
				return errors.Join(err, closeErr, r.Stop())
			}
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-startup.Done():
			timer.Stop()
			return errors.Join(fmt.Errorf("competitor readiness: %w", startup.Err()), r.Stop())
		case <-r.waitDone:
			timer.Stop()
			return errors.Join(fmt.Errorf("competitor exited before readiness: %v", r.waitErr), r.Stop())
		case <-timer.C:
		}
	}
}
