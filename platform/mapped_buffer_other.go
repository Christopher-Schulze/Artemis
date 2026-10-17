//go:build !darwin && !linux

package platform

func mapBuffer(int) ([]byte, error) { return nil, ErrMmapUnavailable }

func unmapBuffer([]byte) error { return ErrMmapUnavailable }
