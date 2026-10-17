package jpegcodec

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// ValidateScanBudget bounds progressive work for both native and Go fallbacks.
// Segment payloads are skipped by their encoded length; an FFDA byte pair in
// metadata is not a scan. Entropy stuffing/restart markers are parsed separately.
func ValidateScanBudget(raw []byte) error {
	if len(raw) < 4 || raw[0] != 0xff || raw[1] != 0xd8 {
		return fmt.Errorf("JPEG start marker required")
	}
	index, scans, entropy := 2, 0, false
	for index < len(raw) {
		if entropy {
			offset := bytes.IndexByte(raw[index:], 0xff)
			if offset < 0 {
				break
			}
			index += offset
		}
		if raw[index] != 0xff {
			return fmt.Errorf("JPEG marker required outside entropy")
		}
		for index < len(raw) && raw[index] == 0xff {
			index++
		}
		if index == len(raw) {
			break
		}
		marker := raw[index]
		index++
		if entropy && (marker == 0 || marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		entropy = false
		if marker == 0xd9 {
			if scans == 0 {
				return fmt.Errorf("JPEG has no image scans")
			}
			return nil
		}
		if marker == 0 || marker == 0xd8 || marker >= 0xd0 && marker <= 0xd7 {
			return fmt.Errorf("invalid JPEG structural marker")
		}
		if marker == 1 {
			continue
		}
		if len(raw)-index < 2 {
			break
		}
		length := int(binary.BigEndian.Uint16(raw[index : index+2]))
		if length < 2 || length > len(raw)-index {
			return fmt.Errorf("invalid JPEG segment length")
		}
		index += length
		if marker == 0xda {
			scans++
			if scans > 100 {
				return fmt.Errorf("JPEG exceeds 100-scan work budget")
			}
			entropy = true
		}
	}
	return fmt.Errorf("JPEG end marker required")
}
