package network

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type robotsPattern struct {
	literal     string
	exact       bool
	wildcard    *regexp.Regexp
	specificity int
}

func compileRobotsPattern(raw string) (robotsPattern, error) {
	if len(raw) > 8192 || !utf8.ValidString(raw) {
		return robotsPattern{}, errors.New("robots: invalid rule pattern")
	}
	exact := strings.HasSuffix(raw, "$")
	raw = strings.TrimSuffix(raw, "$")
	normalized := normalizeRobotsOctets(raw, true)
	pattern := robotsPattern{literal: normalized, exact: exact, specificity: len(strings.ReplaceAll(normalized, "*", ""))}
	if !strings.Contains(normalized, "*") {
		return pattern, nil
	}
	parts := strings.Split(normalized, "*")
	for index := range parts {
		parts[index] = regexp.QuoteMeta(parts[index])
	}
	expression := "^" + strings.Join(parts, ".*")
	if exact {
		expression += "$"
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return robotsPattern{}, err
	}
	pattern.wildcard = compiled
	return pattern, nil
}

func (p robotsPattern) matches(path string) bool {
	if p.wildcard != nil {
		return p.wildcard.MatchString(path)
	}
	if p.exact {
		return path == p.literal
	}
	return strings.HasPrefix(path, p.literal)
}

func normalizeRobotsOctets(value string, pattern bool) string {
	const hex = "0123456789ABCDEF"
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '%' && index+2 < len(value) {
			high, low := robotsHex(value[index+1]), robotsHex(value[index+2])
			if high >= 0 && low >= 0 {
				decoded := byte(high*16 + low)
				if robotsUnreserved(decoded) {
					result.WriteByte(decoded)
				} else {
					result.WriteByte('%')
					result.WriteByte(hex[decoded>>4])
					result.WriteByte(hex[decoded&15])
				}
				index += 2
				continue
			}
		}
		if current < 0x21 || current >= 0x7f || current == '$' || current == '*' && !pattern {
			result.WriteByte('%')
			result.WriteByte(hex[current>>4])
			result.WriteByte(hex[current&15])
			continue
		}
		result.WriteByte(current)
	}
	return result.String()
}

func robotsHex(value byte) int {
	if value >= '0' && value <= '9' {
		return int(value - '0')
	}
	if value >= 'a' && value <= 'f' {
		return int(value - 'a' + 10)
	}
	if value >= 'A' && value <= 'F' {
		return int(value - 'A' + 10)
	}
	return -1
}

func robotsUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-._~", rune(value))
}
