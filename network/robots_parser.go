package network

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxRobotsDocumentBytes int64 = 1 << 20

type robotsParser struct {
	policy       *RobotsPolicy
	agents       []string
	rules        []RobotsRule
	delay        time.Duration
	rulesSeen    bool
	assignments  int
	wildcards    int
	payloadBytes int
}

func newRobotsParser() *robotsParser {
	return &robotsParser{policy: &RobotsPolicy{Groups: make(map[string][]RobotsRule), CrawlDelays: make(map[string]time.Duration), patterns: make(map[string]robotsPattern)}}
}

func (p *robotsParser) record(key, value string) error {
	if !utf8.ValidString(value) {
		return nil
	}
	switch key {
	case "user-agent":
		return p.addAgent(value)
	case "allow", "disallow":
		return p.addRule(key == "allow", value)
	case "crawl-delay":
		if len(p.agents) == 0 || strings.ContainsAny(value, "+-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return nil
		}
		delay, err := time.ParseDuration(value + "s")
		if err == nil && delay >= 0 && delay <= 24*time.Hour && delay > p.delay {
			p.delay = delay
		}
	case "sitemap":
		parsed, err := parsePolicyURL(value)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil
		}
		if _, err := normalizePolicyHost(parsed.Hostname()); err != nil {
			return nil
		}
		port, err := policyPort(parsed)
		if err != nil || port < 1 || port > 65535 {
			return nil
		}
		if len(p.policy.Sitemaps) >= 128 {
			return errors.New("robots: sitemap limit exceeded")
		}
		sitemap := parsed.String()
		p.payloadBytes += len(sitemap) + 16
		if p.payloadBytes > 4<<20 {
			return errors.New("robots: policy payload limit exceeded")
		}
		p.policy.Sitemaps = append(p.policy.Sitemaps, sitemap)
	}
	return nil
}

func (p *robotsParser) addAgent(agent string) error {
	agent = strings.ToLower(agent)
	if agent != "*" {
		if agent == "" || len(agent) > 128 {
			return nil
		}
		for _, char := range agent {
			if char >= 'a' && char <= 'z' || char == '_' || char == '-' {
				continue
			}
			return nil
		}
	}
	if p.rulesSeen {
		if err := p.finishGroup(); err != nil {
			return err
		}
	}
	for _, existing := range p.agents {
		if existing == agent {
			return nil
		}
	}
	if len(p.agents) >= 32 {
		return errors.New("robots: consecutive agent limit exceeded")
	}
	p.agents = append(p.agents, agent)
	return nil
}

func (p *robotsParser) addRule(allow bool, path string) error {
	if len(p.agents) == 0 {
		return nil
	}
	p.rulesSeen = true
	if path == "" {
		return nil
	}
	pattern, exists := p.policy.patterns[path]
	if !exists {
		var err error
		pattern, err = compileRobotsPattern(path)
		if err != nil {
			return err
		}
		if pattern.wildcard != nil {
			p.wildcards++
			if p.wildcards > 1024 {
				return errors.New("robots: wildcard program limit exceeded")
			}
		}
		p.policy.patterns[path] = pattern
	}
	p.rules = append(p.rules, RobotsRule{Allow: allow, Path: path})
	return nil
}

func (p *robotsParser) finishGroup() error {
	for _, agent := range p.agents {
		if _, exists := p.policy.Groups[agent]; !exists {
			p.policy.Groups[agent] = nil
			p.payloadBytes += len(agent) + 16
		}
		p.assignments += len(p.rules)
		if len(p.policy.Groups) > 1024 || p.assignments > 32768 {
			return errors.New("robots: group/rule assignment limit exceeded")
		}
		for _, rule := range p.rules {
			p.payloadBytes += len(rule.Path) + 16
		}
		if p.payloadBytes > 4<<20 {
			return errors.New("robots: policy payload limit exceeded")
		}
		p.policy.Groups[agent] = append(p.policy.Groups[agent], p.rules...)
		if p.delay > p.policy.CrawlDelays[agent] {
			if _, exists := p.policy.CrawlDelays[agent]; !exists {
				p.payloadBytes += len(agent) + 8
			}
			if p.payloadBytes > 4<<20 {
				return errors.New("robots: policy payload limit exceeded")
			}
			p.policy.CrawlDelays[agent] = p.delay
		}
	}
	p.agents = nil
	p.rules = nil
	p.delay = 0
	p.rulesSeen = false
	return nil
}
