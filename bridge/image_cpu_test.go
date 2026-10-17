package bridge

import (
	"context"
	"errors"
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observedImageCPU struct {
	held     atomic.Int32
	releases atomic.Int32
	denied   error
	cleanup  error
}

func (a *observedImageCPU) ReserveCPU(_ context.Context, count int) (context.Context, func() error, error) {
	if a.denied != nil {
		return nil, nil, a.denied
	}
	a.held.Add(int32(count))
	return context.Background(), func() error {
		a.held.Add(-int32(count))
		a.releases.Add(1)
		return a.cleanup
	}, nil
}

type heldResizeImage struct {
	image.Image
	owner   *observedImageCPU
	entered chan struct{}
	finish  chan struct{}
	once    sync.Once
	unowned atomic.Bool
}

func (i *heldResizeImage) At(x, y int) color.Color {
	i.once.Do(func() { close(i.entered) })
	<-i.finish
	if i.owner.held.Load() != 2 {
		i.unowned.Store(true)
	}
	return i.Image.At(x, y)
}

func TestImageCPUCallerCancellationRetainsBlockedPixels(t *testing.T) {
	owner := &observedImageCPU{}
	img := &heldResizeImage{Image: image.NewNRGBA(image.Rect(0, 0, 64, 32)), owner: owner, entered: make(chan struct{}), finish: make(chan struct{})}
	pipeline := NewJPEGPipeline()
	pipeline.MaxWidth, pipeline.MaxHeight = 16, 16
	pipeline.CPU = ImageCPUConfig{Admission: owner, RequireAdmission: true, Workers: 2}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := pipeline.ResizeForModelContext(ctx, img)
		if result != nil {
			err = errors.Join(err, errors.New("canceled image published pixels"))
		}
		done <- err
	}()
	select {
	case <-img.entered:
	case <-time.After(2 * time.Second):
		close(img.finish)
		t.Fatal("real pixels never reached the admitted worker")
	}
	cancel()
	select {
	case err := <-done:
		close(img.finish)
		t.Fatalf("image ownership released before blocked pixel operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if owner.held.Load() != 2 || owner.releases.Load() != 0 {
		close(img.finish)
		t.Fatal("live pixel operation lost CPU ownership")
	}
	close(img.finish)
	if err := <-done; !errors.Is(err, context.Canceled) || owner.held.Load() != 0 || owner.releases.Load() != 1 || img.unowned.Load() {
		t.Fatalf("caller cancellation/join failed: %v held=%d releases=%d unowned=%t", err, owner.held.Load(), owner.releases.Load(), img.unowned.Load())
	}
}

func TestImageCPUAdmissionAndSettlementFailuresPublishNoPixels(t *testing.T) {
	denied, cleanup := errors.New("capacity denied"), errors.New("settlement failed")
	var absent *observedImageCPU
	for _, test := range []struct {
		name  string
		cpu   ImageCPUConfig
		cause error
	}{
		{"required", ImageCPUConfig{RequireAdmission: true}, nil},
		{"typed_nil", ImageCPUConfig{Admission: absent}, nil},
		{"denied", ImageCPUConfig{Admission: &observedImageCPU{denied: denied}}, denied},
		{"settlement", ImageCPUConfig{Admission: &observedImageCPU{cleanup: cleanup}}, cleanup},
		{"too_many_workers", ImageCPUConfig{Workers: 5}, nil},
		{"negative_workers", ImageCPUConfig{Workers: -1}, nil},
		{"too_many_cpus", ImageCPUConfig{Workers: 1, Affinity: []int{0, 1}}, nil},
		{"invalid_cpu", ImageCPUConfig{Workers: 1, Affinity: []int{-1}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			pipeline := NewJPEGPipeline()
			pipeline.MaxWidth, pipeline.MaxHeight, pipeline.CPU = 16, 16, test.cpu
			result, err := pipeline.ResizeForModelContext(t.Context(), image.NewNRGBA(image.Rect(0, 0, 64, 32)))
			if err == nil || result != nil || (test.cause != nil && !errors.Is(err, test.cause)) {
				t.Fatalf("CPU admission/settlement bypass: result=%T err=%v", result, err)
			}
			if owner, ok := test.cpu.Admission.(*observedImageCPU); ok && owner != nil && owner.held.Load() != 0 {
				t.Fatal("failed resize retained live CPU capacity")
			}
		})
	}
}
