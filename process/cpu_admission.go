package process

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"syscall"
	"time"
)

// CPUAdmission supplies the embedding application's existing capacity owner.
// Cancellation requests stop; only joined process-group exit permits release.
type CPUAdmission interface {
	ReserveCPU(ctx context.Context, count int) (context.Context, func() error, error)
}

func reserveLaunchCPU(ctx context.Context, config LaunchConfig) (context.Context, func() error, error) {
	if config.CPUAdmission == nil {
		if config.RequireCPUAdmission || config.CPUCount > 0 || len(config.CPUAffinity) != 0 {
			return nil, nil, invalidConfig("CPU reservation authority required")
		}
		return ctx, nil, nil
	}
	value := reflect.ValueOf(config.CPUAdmission)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, nil, invalidConfig("typed-nil CPU admission")
		}
	}
	count := max(1, config.CPUCount, len(config.CPUAffinity))
	leaseCtx, release, err := config.CPUAdmission.ReserveCPU(ctx, count)
	if err != nil {
		return nil, nil, fmt.Errorf("reserve Chromium CPU: %w", err)
	}
	if leaseCtx == nil || release == nil {
		if release != nil {
			err = release()
		}
		return nil, nil, errors.Join(invalidConfig("CPU authority returned incomplete ownership"), err)
	}
	if err := errors.Join(ctx.Err(), leaseCtx.Err()); err != nil {
		return nil, nil, errors.Join(err, release())
	}
	// Preserve the caller's stop boundary even if an injected owner returns
	// a context that does not derive from the caller.
	boundCtx, cancel := context.WithCancelCause(leaseCtx)
	stopParent := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	return boundCtx, func() error {
		stopParent()
		cancel(nil)
		return release()
	}, nil
}

func (b *Browser) settleCPU() error {
	if b.releaseCPU == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.shutdown)
	defer cancel()
	stopped, err := processGroupCPUStopped(ctx, b.cmd.Process.Pid)
	if err != nil {
		return err
	}
	if stopped {
		return b.releaseCPU()
	}
	if err := SignalProcessGroup(b.cmd.Process, syscall.SIGKILL); err != nil {
		// Group exit can race the signal, including EPERM for inert groups.
		// Only independent kernel-state proof permits releasing that owner.
		stopped, observeErr := processGroupCPUStopped(ctx, b.cmd.Process.Pid)
		if observeErr == nil && stopped {
			return b.releaseCPU()
		}
		return errors.Join(fmt.Errorf("stop CPU owner process group: %w", err), observeErr)
	}
	for {
		stopped, err := processGroupCPUStopped(ctx, b.cmd.Process.Pid)
		if err != nil {
			return err
		}
		if stopped {
			return b.releaseCPU()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("CPU owner process group exit unproved: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
