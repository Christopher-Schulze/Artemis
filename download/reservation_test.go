package download

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

type reservationProbe struct {
	mu       sync.Mutex
	admitted bool
	admit    int
	releases int
}

func (p *reservationProbe) Admit(_ context.Context, _ string, _ string, _ StorageReservationSpec) (AdmissionResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.admit++
	return AdmissionResult{Admitted: p.admitted, ByteDeficit: 4, Reason: "probe denied"}, nil
}

func (p *reservationProbe) Release(_ context.Context, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releases++
	return nil
}

func TestDownloadManagerRequiresAndReleasesSharedReservation(t *testing.T) {
	policy := newTestPolicy(t, 64, []string{"*/*"})
	if _, err := NewDownloadManager(DownloadConfig{
		RootDir: t.TempDir(), SessionID: "required", Policy: policy, RequireReservation: true,
	}); err == nil {
		t.Fatal("manager without required reservation authority was accepted")
	}
	probe := &reservationProbe{admitted: true}
	manager, err := NewDownloadManager(DownloadConfig{
		RootDir: t.TempDir(), SessionID: "reserved", MaxDiskBytes: 1024, MinFreeBytes: 1,
		Policy: policy, Reservation: probe, RequireReservation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StoreContext(context.Background(), "ok.txt", "text/plain", []byte("reserved")); err != nil {
		t.Fatal(err)
	}
	probe.mu.Lock()
	admitCalls, releaseCalls := probe.admit, probe.releases
	probe.mu.Unlock()
	if admitCalls != 1 || releaseCalls != 1 {
		t.Fatalf("reservation calls admit=%d release=%d", admitCalls, releaseCalls)
	}
}

func TestDownloadManagerReservationDenialLeavesNoFile(t *testing.T) {
	probe := &reservationProbe{}
	manager, err := NewDownloadManager(DownloadConfig{
		RootDir: t.TempDir(), SessionID: "denied", MaxDiskBytes: 1024, MinFreeBytes: 1,
		Policy: newTestPolicy(t, 64, []string{"*/*"}), Reservation: probe, RequireReservation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.StoreContext(context.Background(), "denied.txt", "text/plain", []byte("denied"))
	if err == nil || !strings.Contains(err.Error(), "shared storage reservation") {
		t.Fatalf("denied Store err=%v", err)
	}
	if _, statErr := os.Stat(manager.Directory() + "/denied.txt"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("denied file remains: %v", statErr)
	}
}
