package security

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// adblock_rules.go is the Adblock Plus network-filter engine for Layer 1
// (spec L4206: ABP filter syntax). It implements the request-blocking subset
// of the syntax (anchors `||` `|`, wildcard `*`, separator `^`, exceptions
// `@@`, regex filters and the options $third-party, $domain/$from,
// resource types, $match-case and $important) with a token index, so a
// request is matched against a handful of candidate rules instead of the
// whole list. Element hiding and response-rewriting filters ($csp,
// $redirect, $removeparam, ...) are counted as unsupported and skipped:
// applying only their blocking half would change page semantics.

// AdBlockResource is the request class matched by ABP resource-type options.
type AdBlockResource uint16

const (
	ResourceOther AdBlockResource = 1 << iota
	ResourceScript
	ResourceImage
	ResourceStylesheet
	ResourceObject
	ResourceXHR
	ResourceSubdocument
	ResourcePing
	ResourceMedia
	ResourceFont
	ResourceWebSocket
)

const allAdBlockResources = ResourceOther | ResourceScript | ResourceImage | ResourceStylesheet | ResourceObject |
	ResourceXHR | ResourceSubdocument | ResourcePing | ResourceMedia | ResourceFont | ResourceWebSocket

var adBlockResourceOptions = map[string]AdBlockResource{
	"script": ResourceScript, "image": ResourceImage, "stylesheet": ResourceStylesheet, "css": ResourceStylesheet,
	"object": ResourceObject, "object-subrequest": ResourceObject, "xmlhttprequest": ResourceXHR, "xhr": ResourceXHR,
	"subdocument": ResourceSubdocument, "frame": ResourceSubdocument, "ping": ResourcePing, "beacon": ResourcePing,
	"media": ResourceMedia, "font": ResourceFont, "websocket": ResourceWebSocket, "other": ResourceOther,
}

// AdBlockRequest describes one request for rule matching. Host and SiteHost
// are lowercase hostnames; ThirdParty compares their registrable domains.
type AdBlockRequest struct {
	URL        string
	Host       string
	SiteHost   string
	Resource   AdBlockResource
	ThirdParty bool
}

// AdBlockRuleStats counts what a rule list contributed.
type AdBlockRuleStats struct {
	Lines       int `json:"lines"`
	Blocking    int `json:"blocking"`
	Exceptions  int `json:"exceptions"`
	Cosmetic    int `json:"cosmetic"`
	Unsupported int `json:"unsupported"`
}

type networkRule struct {
	raw          string
	pattern      string // lowercase unless matchCase; '*' and '^' are meta
	regex        *regexp.Regexp
	domainAnchor bool
	startAnchor  bool
	endAnchor    bool
	matchCase    bool
	important    bool
	thirdParty   int8 // 1 third-party only, -1 first-party only
	resources    AdBlockResource
	domains      []string
	notDomains   []string
}

type ruleIndex struct {
	byHost  map[string][]*networkRule // `||host^` rules keyed by host
	byToken map[string][]*networkRule
	generic []*networkRule
}

// AdBlockRuleSet is an immutable compiled filter list, safe for concurrent
// matching.
type AdBlockRuleSet struct {
	block     ruleIndex
	allow     ruleIndex
	important ruleIndex
	stats     AdBlockRuleStats
}

// ParseAdBlockRules compiles ABP filter lines from r.
func ParseAdBlockRules(r io.Reader) (*AdBlockRuleSet, error) {
	set := &AdBlockRuleSet{block: newRuleIndex(), allow: newRuleIndex(), important: newRuleIndex()}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		set.addLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

// Stats reports how many lines became rules.
func (s *AdBlockRuleSet) Stats() AdBlockRuleStats { return s.stats }

func newRuleIndex() ruleIndex {
	return ruleIndex{byHost: map[string][]*networkRule{}, byToken: map[string][]*networkRule{}}
}

func (s *AdBlockRuleSet) addLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '!' || line[0] == '[' {
		return
	}
	s.stats.Lines++
	if isCosmeticFilter(line) {
		s.stats.Cosmetic++
		return
	}
	exception := strings.HasPrefix(line, "@@")
	rule, ok := compileNetworkRule(line, exception)
	if !ok {
		s.stats.Unsupported++
		return
	}
	switch {
	case exception:
		s.stats.Exceptions++
		s.allow.add(rule)
	case rule.important:
		s.stats.Blocking++
		s.important.add(rule)
	default:
		s.stats.Blocking++
		s.block.add(rule)
	}
}

func isCosmeticFilter(line string) bool {
	for _, marker := range []string{"##", "#@#", "#?#", "#$#", "#%#", "#@$#", "#@?#", "$$", "$@$"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func compileNetworkRule(line string, exception bool) (*networkRule, bool) {
	rule := &networkRule{raw: line, resources: allAdBlockResources}
	body := line
	if exception {
		body = body[2:]
	}
	if options, pattern, hasOptions := splitRuleOptions(body); hasOptions {
		body = pattern
		if !rule.applyOptions(options) {
			return nil, false
		}
	}
	if len(body) >= 2 && body[0] == '/' && body[len(body)-1] == '/' {
		expression := body[1 : len(body)-1]
		if !rule.matchCase {
			expression = "(?i)" + expression
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return nil, false
		}
		rule.regex = compiled
		return rule, true
	}
	switch {
	case strings.HasPrefix(body, "||"):
		rule.domainAnchor, body = true, body[2:]
	case strings.HasPrefix(body, "|"):
		rule.startAnchor, body = true, body[1:]
	}
	if strings.HasSuffix(body, "|") {
		rule.endAnchor, body = true, body[:len(body)-1]
	}
	if !rule.matchCase {
		body = strings.ToLower(body)
	}
	if strings.Trim(body, "*^") == "" && !rule.domainAnchor {
		return nil, false // matches every request; never a valid blocking filter
	}
	rule.pattern = body
	return rule, true
}

// splitRuleOptions splits "pattern$opt,opt"; a '$' inside a regex body is not
// an option separator.
func splitRuleOptions(body string) (options []string, pattern string, ok bool) {
	index := strings.LastIndexByte(body, '$')
	if index < 0 || (strings.HasPrefix(body, "/") && strings.HasSuffix(body, "/")) {
		return nil, body, false
	}
	return strings.Split(body[index+1:], ","), body[:index], true
}

func (r *networkRule) applyOptions(options []string) bool {
	var included, excluded AdBlockResource
	for _, option := range options {
		option = strings.TrimSpace(option)
		negated := strings.HasPrefix(option, "~")
		name := strings.ToLower(strings.TrimPrefix(option, "~"))
		value := ""
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, value = name[:eq], option[strings.IndexByte(option, '=')+1:]
		}
		switch name {
		case "third-party", "3p":
			r.thirdParty = map[bool]int8{false: 1, true: -1}[negated]
		case "first-party", "1p":
			r.thirdParty = map[bool]int8{false: -1, true: 1}[negated]
		case "domain", "from":
			if !r.applyDomains(value) {
				return false
			}
		case "match-case":
			r.matchCase = true
		case "important":
			r.important = true
		default:
			resource, known := adBlockResourceOptions[name]
			if !known || value != "" {
				return false
			}
			if negated {
				excluded |= resource
			} else {
				included |= resource
			}
		}
	}
	if included != 0 {
		r.resources = included
	}
	r.resources &^= excluded
	return r.resources != 0
}

func (r *networkRule) applyDomains(value string) bool {
	for _, domain := range strings.Split(value, "|") {
		domain = strings.ToLower(strings.TrimSpace(domain))
		negated := strings.HasPrefix(domain, "~")
		domain = strings.TrimPrefix(domain, "~")
		if domain == "" || strings.HasSuffix(domain, ".*") {
			return false
		}
		if negated {
			r.notDomains = append(r.notDomains, domain)
		} else {
			r.domains = append(r.domains, domain)
		}
	}
	return true
}

func (ix *ruleIndex) add(rule *networkRule) {
	if rule.regex == nil && rule.domainAnchor {
		if host, ok := plainHostPattern(rule.pattern); ok {
			ix.byHost[host] = append(ix.byHost[host], rule)
			return
		}
	}
	if token := ruleToken(rule); token != "" {
		ix.byToken[token] = append(ix.byToken[token], rule)
		return
	}
	ix.generic = append(ix.generic, rule)
}

// plainHostPattern recognizes `host`, `host^` and `host/` bodies.
func plainHostPattern(pattern string) (string, bool) {
	host := strings.TrimSuffix(strings.TrimSuffix(pattern, "^"), "/")
	if host == "" || strings.IndexAny(host, "*^/:?=&") >= 0 {
		return "", false
	}
	return strings.ToLower(host), true
}

// ruleToken picks the longest literal token that is a complete URL token in
// every URL the rule matches: bounded on both sides by a non-token literal,
// a separator or an anchor, never by a wildcard or an unanchored edge.
func ruleToken(rule *networkRule) string {
	if rule.regex != nil {
		return ""
	}
	pattern := strings.ToLower(rule.pattern)
	best := ""
	for start := 0; start < len(pattern); {
		if !isTokenByte(pattern[start]) {
			start++
			continue
		}
		end := start
		for end < len(pattern) && isTokenByte(pattern[end]) {
			end++
		}
		leftBounded := start > 0 && pattern[start-1] != '*' || start == 0 && (rule.domainAnchor || rule.startAnchor)
		rightBounded := end < len(pattern) && pattern[end] != '*' || end == len(pattern) && rule.endAnchor
		if leftBounded && rightBounded && end-start > len(best) {
			best = pattern[start:end]
		}
		start = end
	}
	if len(best) < 2 {
		return ""
	}
	return best
}

func isTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '%' || c >= 'A' && c <= 'Z'
}

// Match reports whether request is blocked and by which filter.
func (s *AdBlockRuleSet) Match(request AdBlockRequest) (bool, string) {
	if s == nil {
		return false, ""
	}
	lowerURL := strings.ToLower(request.URL)
	tokens := urlTokens(lowerURL)
	if rule := s.important.find(request, lowerURL, tokens); rule != nil {
		return true, rule.raw
	}
	rule := s.block.find(request, lowerURL, tokens)
	if rule == nil || s.allow.find(request, lowerURL, tokens) != nil {
		return false, ""
	}
	return true, rule.raw
}

func urlTokens(lowerURL string) []string {
	tokens := make([]string, 0, 16)
	for start := 0; start < len(lowerURL); {
		if !isTokenByte(lowerURL[start]) {
			start++
			continue
		}
		end := start
		for end < len(lowerURL) && isTokenByte(lowerURL[end]) {
			end++
		}
		if end-start >= 2 {
			tokens = append(tokens, lowerURL[start:end])
		}
		start = end
	}
	return tokens
}

func (ix *ruleIndex) find(request AdBlockRequest, lowerURL string, tokens []string) *networkRule {
	for host := request.Host; host != ""; {
		for _, rule := range ix.byHost[host] {
			if rule.optionsMatch(request) {
				return rule
			}
		}
		dot := strings.IndexByte(host, '.')
		if dot < 0 {
			break
		}
		host = host[dot+1:]
	}
	for _, token := range tokens {
		for _, rule := range ix.byToken[token] {
			if rule.optionsMatch(request) && rule.matches(request, lowerURL) {
				return rule
			}
		}
	}
	for _, rule := range ix.generic {
		if rule.optionsMatch(request) && rule.matches(request, lowerURL) {
			return rule
		}
	}
	return nil
}

func (r *networkRule) optionsMatch(request AdBlockRequest) bool {
	if r.resources&request.Resource == 0 {
		return false
	}
	if r.thirdParty == 1 && !request.ThirdParty || r.thirdParty == -1 && request.ThirdParty {
		return false
	}
	if len(r.notDomains) > 0 && hostInDomains(request.SiteHost, r.notDomains) {
		return false
	}
	return len(r.domains) == 0 || hostInDomains(request.SiteHost, r.domains)
}

func hostInDomains(host string, domains []string) bool {
	for _, domain := range domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (r *networkRule) matches(request AdBlockRequest, lowerURL string) bool {
	target := lowerURL
	if r.matchCase {
		target = request.URL
	}
	if r.regex != nil {
		return r.regex.MatchString(target)
	}
	if r.startAnchor {
		return globMatch(r.pattern, target, r.endAnchor)
	}
	if r.domainAnchor {
		hostStart := strings.Index(target, "://")
		if hostStart < 0 {
			return false
		}
		hostStart += 3
		hostEnd := hostStart + len(request.Host)
		for position := hostStart; position <= hostEnd && position < len(target); position++ {
			if (position == hostStart || target[position-1] == '.') && globMatch(r.pattern, target[position:], r.endAnchor) {
				return true
			}
		}
		return false
	}
	for position := 0; position < len(target); position++ {
		if globMatch(r.pattern, target[position:], r.endAnchor) {
			return true
		}
	}
	return false
}

// globMatch matches pattern at the start of s; '*' matches any run and '^' a
// separator character or the end of s. Without anchorEnd any suffix may
// remain.
func globMatch(pattern, s string, anchorEnd bool) bool {
	pi, si, starP, starS := 0, 0, -1, 0
	for {
		if pi == len(pattern) {
			if !anchorEnd || si == len(s) {
				return true
			}
		} else {
			switch c := pattern[pi]; c {
			case '*':
				starP, starS = pi, si
				pi++
				continue
			case '^':
				if si < len(s) && isSeparator(s[si]) {
					pi, si = pi+1, si+1
					continue
				}
				if si == len(s) {
					pi++
					continue
				}
			default:
				if si < len(s) && s[si] == c {
					pi, si = pi+1, si+1
					continue
				}
			}
		}
		if starP < 0 || starS >= len(s) {
			return false
		}
		starS++
		pi, si = starP+1, starS
	}
}

func isSeparator(c byte) bool {
	return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '%')
}
