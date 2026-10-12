package scraper

import (
	"bytes"
	"io"
)

// TeeParseReaders duplicates readers into independent replay buffers.
func TeeParseReaders(primary io.Reader, secondary io.Reader) (io.Reader, io.Reader, error) {
	if primary == nil || secondary == nil {
		return nil, nil, io.ErrUnexpectedEOF
	}
	pBytes, err := io.ReadAll(primary)
	if err != nil {
		return nil, nil, err
	}
	sBytes, err := io.ReadAll(secondary)
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(pBytes)), io.NopCloser(bytes.NewReader(sBytes)), nil
}
