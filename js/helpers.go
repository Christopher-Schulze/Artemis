package js

import "strings"

// itoa formats a uint32 as decimal without allocations.
func itoa(u uint32) string {
	if u == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	return string(buf[i:])
}

// jsStringLit produces a JS-safe double-quoted string literal.
//
// Optimization (TASK-2344): pre-size the strings.Builder to len(s)+2
// (the minimum output: 2 quotes + input unchanged) to avoid the
// Builder's internal buffer reallocations.
func jsStringLit(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
