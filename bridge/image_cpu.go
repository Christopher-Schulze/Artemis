package bridge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sync"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// ImageCPUAdmission retains weighted capacity until all image workers finish.
type ImageCPUAdmission interface {
	ReserveCPU(context.Context, int) (context.Context, func() error, error)
}

// ImageAdmissionError preserves the capacity owner's cause while separating
// denied work from decode, encode and settlement failure in operational data.
type ImageAdmissionError struct {
	Cause error
}

func (e *ImageAdmissionError) Error() string { return e.Cause.Error() }
func (e *ImageAdmissionError) Unwrap() error { return e.Cause }

// ImageCPUConfig supplies an embedding owner's governed worker allocation.
// Zero Workers selects at most four workers; absent admission uses that local
// bound without claiming shared capacity or physical CPU ownership.
type ImageCPUConfig struct {
	Admission        ImageCPUAdmission
	RequireAdmission bool
	Workers          int
	Affinity         []int
}

func (p *JPEGPipeline) imageWorkerCount() int {
	if p.CPU.Workers != 0 {
		return p.CPU.Workers
	}
	return min(4, max(1, runtime.GOMAXPROCS(0)))
}

func (p *JPEGPipeline) withImageCPU(ctx context.Context, operation func(context.Context, *JPEGPipeline) error) (proof platform.ProbeResult, err error) {
	if p == nil || ctx == nil || operation == nil || p.imageWorkerCount() < 1 || p.imageWorkerCount() > 4 {
		return proof, fmt.Errorf("image CPU: context, pipeline and 1..4 workers required")
	}
	if err := ctx.Err(); err != nil {
		return proof, err
	}
	owned := *p
	owned.CPU.Workers = p.imageWorkerCount()
	owned.CPU.Affinity = slices.Clone(p.CPU.Affinity)
	if len(owned.CPU.Affinity) > owned.imageWorkerCount() {
		return proof, fmt.Errorf("image CPU: affinity exceeds worker allocation")
	}
	owner := owned.CPU.Admission
	if owner == nil && owned.CPU.RequireAdmission {
		return proof, fmt.Errorf("image CPU: shared admission required")
	}
	if owner != nil {
		value := reflect.ValueOf(owner)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return proof, fmt.Errorf("image CPU: typed-nil admission")
			}
		}
	}
	workCtx := ctx
	if owner != nil {
		reserved, release, admissionErr := owner.ReserveCPU(ctx, owned.imageWorkerCount())
		if release != nil {
			defer func() { err = errors.Join(err, release()) }()
		}
		if admissionErr != nil {
			return proof, &ImageAdmissionError{Cause: fmt.Errorf("admit image CPU: %w", admissionErr)}
		}
		if reserved == nil || release == nil {
			return proof, fmt.Errorf("image CPU: admission context and release required")
		}
		var cancel context.CancelCauseFunc
		workCtx, cancel = context.WithCancelCause(reserved)
		stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
		defer stop()
		defer cancel(nil)
		if err := ctx.Err(); err != nil {
			cancel(context.Cause(ctx))
			return proof, err
		}
	}
	proof, err = platform.RunCPUAffinity(workCtx, owned.CPU.Affinity, func(workerCtx context.Context) error {
		return operation(workerCtx, &owned)
	})
	return proof, errors.Join(err, context.Cause(workCtx), ctx.Err())
}

// runImageWorkers partitions lines without an image-sized task queue. Every
// actual worker binds its own OS thread and is joined even after cancellation,
// panic or Goexit. The embedding capacity cannot be settled before this join.
func runImageWorkers(ctx context.Context, workers, lines int, affinity []int, operation func(context.Context, int, int) error) error {
	if ctx == nil || operation == nil || workers < 1 || workers > 4 || lines < 1 {
		return fmt.Errorf("image workers: context, operation and bounded dimensions required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	workers = min(workers, lines)
	workCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	failures := make([]error, workers)
	var joined sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		joined.Add(1)
		go func() {
			finished := false
			defer joined.Done()
			defer func() {
				if recovered := recover(); recovered != nil || !finished {
					failures[worker] = errors.Join(failures[worker], fmt.Errorf("image worker did not complete normally"))
				}
				if failures[worker] != nil {
					cancel(failures[worker])
				}
			}()
			_, failures[worker] = platform.RunCPUAffinity(workCtx, affinity, func(boundCtx context.Context) error {
				return operation(boundCtx, worker, workers)
			})
			finished = true
		}()
	}
	joined.Wait()
	return errors.Join(errors.Join(failures...), context.Cause(workCtx))
}
