package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/serve"
)

const (
	smokeReadinessTimeout = 10 * time.Second
	smokeShutdownTimeout  = 10 * time.Second
	smokeLogBytesLimit    = 1 << 20
	readinessBytesLimit   = 4096
)

type boundedSmokeLog struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
}

func (b *boundedSmokeLog) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := min(len(data), smokeLogBytesLimit-b.buffer.Len())
	if limit < len(data) {
		b.overflow = true
	}
	if _, err := b.buffer.Write(data[:limit]); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (b *boundedSmokeLog) snapshot() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflow {
		return bytes.Clone(b.buffer.Bytes()), fmt.Errorf("serve log exceeds %d bytes", smokeLogBytesLimit)
	}
	return bytes.Clone(b.buffer.Bytes()), nil
}

type ownedSmokeServe struct {
	command   *exec.Cmd
	stdout    io.ReadCloser
	logs      boundedSmokeLog
	done      chan struct{}
	waitErr   error
	readyDone chan struct{}
	ready     serve.Readiness
	readyErr  error
	cancel    context.CancelFunc
}

func (r *Runner) startServe(ctx context.Context) (*ownedSmokeServe, *serve.Client, error) {
	if err := r.verifyBinary(); err != nil {
		return nil, nil, err
	}
	processCtx, processCancel := context.WithCancel(ctx)
	command := newSmokeCommand(processCtx, r.binPath, "serve", "--host", r.cfg.Host, "--port", fmt.Sprint(r.cfg.Port), "--ready-json")
	browserprocess.ConfigureProcessGroup(command)
	command.Cancel = func() error { return browserprocess.SignalProcessGroup(command.Process, syscall.SIGTERM) }
	command.WaitDelay = smokeShutdownTimeout
	command.Env = append(os.Environ(), "ARTEMIS_SERVE_TOKEN="+r.cfg.AuthToken)
	owner := &ownedSmokeServe{command: command, done: make(chan struct{}), readyDone: make(chan struct{})}
	owner.cancel = processCancel
	command.Stderr = &owner.logs
	stdout, err := command.StdoutPipe()
	if err != nil {
		processCancel()
		return nil, nil, err
	}
	owner.stdout = stdout
	if err := command.Start(); err != nil {
		processCancel()
		closeErr := stdout.Close()
		if errors.Is(closeErr, os.ErrClosed) {
			closeErr = nil
		}
		return nil, nil, errors.Join(err, closeErr)
	}
	r.children = append(r.children, owner)
	go func() { owner.waitErr = command.Wait(); close(owner.done) }()
	go func() {
		decoder := json.NewDecoder(io.LimitReader(stdout, readinessBytesLimit))
		decoder.DisallowUnknownFields()
		owner.readyErr = decoder.Decode(&owner.ready)
		close(owner.readyDone)
	}()
	readyCtx, cancel := context.WithTimeout(ctx, smokeReadinessTimeout)
	defer cancel()
	select {
	case <-owner.readyDone:
		if owner.readyErr != nil {
			return owner, nil, fmt.Errorf("decode owned serve readiness: %w", owner.readyErr)
		}
	case <-owner.done:
		return owner, nil, errors.Join(fmt.Errorf("serve exited before readiness"), owner.waitErr)
	case <-readyCtx.Done():
		return owner, nil, fmt.Errorf("owned serve readiness: %w", readyCtx.Err())
	}
	if err := owner.ready.Validate(); err != nil {
		return owner, nil, err
	}
	host, port, err := net.SplitHostPort(owner.ready.Address)
	if err != nil {
		return owner, nil, err
	}
	if owner.ready.ProcessID != command.Process.Pid || host != r.cfg.Host || (r.cfg.Port != 0 && port != fmt.Sprint(r.cfg.Port)) {
		return owner, nil, fmt.Errorf("serve readiness belongs to another process or endpoint")
	}
	client, err := serve.Dial(readyCtx, "ws://"+owner.ready.Address+"/", r.cfg.AuthToken, "")
	if err != nil {
		return owner, nil, err
	}
	if client.ServerInstanceID() != owner.ready.InstanceID || client.ServerProcessID() != command.Process.Pid {
		return owner, nil, errors.Join(fmt.Errorf("authenticated serve instance differs from owned readiness"), client.Close())
	}
	if err := waitForHealth(readyCtx, "http://"+owner.ready.Address+"/healthz", smokeReadinessTimeout); err != nil {
		return owner, nil, errors.Join(err, client.Close())
	}
	select {
	case <-owner.done:
		return owner, nil, errors.Join(fmt.Errorf("serve exited during ownership verification"), owner.waitErr, client.Close())
	default:
		return owner, client, nil
	}
}

func (o *ownedSmokeServe) close() error {
	defer o.cancel()
	var stopErr error
	select {
	case <-o.done:
		stopErr = fmt.Errorf("serve exited before owned shutdown")
	default:
		stopErr = browserprocess.SignalProcessGroup(o.command.Process, syscall.SIGTERM)
		select {
		case <-o.done:
		case <-time.After(smokeShutdownTimeout):
			stopErr = errors.Join(stopErr, fmt.Errorf("serve required forced shutdown"), browserprocess.SignalProcessGroup(o.command.Process, syscall.SIGKILL))
			o.cancel()
			select {
			case <-o.done:
			case <-time.After(smokeShutdownTimeout + time.Second):
				return errors.Join(stopErr, fmt.Errorf("serve process wait did not join"))
			}
		}
	}
	closeErr := o.stdout.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}
	<-o.readyDone
	_, logErr := o.logs.snapshot()
	return errors.Join(stopErr, o.waitErr, closeErr, logErr)
}
