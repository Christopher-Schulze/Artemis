package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

type screenshotCPUOwner struct {
	held int
	deny bool
}

func (o *screenshotCPUOwner) ReserveCPU(ctx context.Context, count int) (context.Context, func() error, error) {
	if o.deny {
		return nil, nil, errors.New("CPU capacity denied")
	}
	o.held += count
	return ctx, func() error { o.held -= count; return nil }, nil
}

type cpuObservedScreenshotMemory struct {
	cpu      *screenshotCPUOwner
	expected int
	observed bool
}

func (m *cpuObservedScreenshotMemory) ReserveMemory(_ context.Context, _ int64) (func() error, error) {
	if m.cpu.held != m.expected {
		return nil, errors.New("pixels admitted without the configured CPU owner")
	}
	m.observed = true
	return func() error {
		if m.cpu.held != m.expected {
			return errors.New("CPU released before pixel-memory settlement")
		}
		return nil
	}, nil
}

func TestScreenshotDecodeCPUFactoryOwnsOrdinaryAndModelPixels(t *testing.T) {
	for _, model := range []bool{false, true} {
		for _, denied := range []bool{false, true} {
			cpu := &screenshotCPUOwner{deny: denied}
			memory := &cpuObservedScreenshotMemory{cpu: cpu, expected: 2}
			runtime := &Runtime{inputMemory: memory, imageCPU: func(context.Context) (bridge.ImageCPUConfig, error) {
				return bridge.ImageCPUConfig{Admission: cpu, RequireAdmission: true, Workers: 2}, nil
			}}
			result := runtime.screenshotOutcome(t.Context(), Request{Kind: KindScreenshot, ForModel: model}, "jpeg", screenshotPixels(t, "jpeg", 1280, 640), Evidence{})
			if cpu.held != 0 {
				t.Fatal("screenshot returned with its image CPU held")
			}
			if denied {
				if result.Success || result.Failure != FailurePolicy || len(result.Bytes) != 0 || memory.observed {
					t.Fatalf("CPU denial reached pixels or lost policy classification: %+v", result)
				}
			} else if !result.Success || !memory.observed || result.ModelPrepared != model {
				t.Fatalf("productive screenshot did not use its CPU/memory owners: %+v", result)
			}
		}
	}
}
