package js

import "math"

func checkedInt64ToUint32(value int64) (uint32, bool) {
	if value < 0 || value > math.MaxUint32 {
		return 0, false
	}
	return uint32(value), true
}

func checkedUint32ToInt32(value uint32) (int32, bool) {
	if value > math.MaxInt32 {
		return 0, false
	}
	return int32(value), true
}

func checkedIntToInt32(value int) (int32, bool) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, false
	}
	return int32(value), true
}

func checkedInt64ToInt32(value int64) (int32, bool) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, false
	}
	return int32(value), true
}

func checkedIntToUint32(value int) (uint32, bool) {
	if value < 0 || int64(value) > math.MaxUint32 {
		return 0, false
	}
	return uint32(value), true
}

func checkedInt64ToByte(value int64) (byte, bool) {
	if value < 0 || value > math.MaxUint8 {
		return 0, false
	}
	return byte(value), true
}

func checkedInt64ToUint16(value int64) (uint16, bool) {
	if value < 0 || value > math.MaxUint16 {
		return 0, false
	}
	return uint16(value), true
}
