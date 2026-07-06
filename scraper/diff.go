// Package scraper implements differential re-scrape with conditional GET and region hashing (spec ss28.12b.13).
package scraper

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

// RegionFingerprint captures a hash for a content region.
type RegionFingerprint struct {
	RegionID    string
	ContentHash string
	LastSeenAt  int64
}

// DiffResult is the outcome of a differential scrape check.
type DiffResult struct {
	URL            string
	ChangedRegions []RegionFingerprint
	Unchanged      []RegionFingerprint
	Conditional304 bool
	ETag           string
	LastModified   string
}

// DiffEngine performs conditional GET and region-level diffing.
type DiffEngine struct {
	fingerprints map[string]RegionFingerprint // url|region_id -> fingerprint
}

// NewDiffEngine creates an engine.
func NewDiffEngine() *DiffEngine {
	return &DiffEngine{fingerprints: make(map[string]RegionFingerprint)}
}

// CheckConditionalGET builds headers for a conditional request.
func (e *DiffEngine) CheckConditionalGET(url string) http.Header {
	h := make(http.Header)
	key := url + "|__global__"
	if fp, ok := e.fingerprints[key]; ok {
		if fp.ContentHash != "" {
			h.Set("If-None-Match", fp.ContentHash)
		}
		if fp.LastSeenAt > 0 {
			t := time.Unix(fp.LastSeenAt, 0).UTC().Format(http.TimeFormat)
			h.Set("If-Modified-Since", t)
		}
	}
	return h
}

// RecordGlobalFingerprint stores the ETag/Last-Modified equivalent for a URL.
func (e *DiffEngine) RecordGlobalFingerprint(url, etag string, lastMod time.Time) {
	key := url + "|__global__"
	var hash string
	if etag != "" {
		hash = etag
	} else {
		hash = hashString(lastMod.UTC().Format(http.TimeFormat))
	}
	e.fingerprints[key] = RegionFingerprint{
		RegionID:    "__global__",
		ContentHash: hash,
		LastSeenAt:  time.Now().Unix(),
	}
}

// DiffRegions compares new content regions against stored fingerprints.
func (e *DiffEngine) DiffRegions(url string, regions map[string]string) DiffResult {
	result := DiffResult{URL: url}
	for regionID, content := range regions {
		key := url + "|" + regionID
		hash := hashString(content)
		fp, exists := e.fingerprints[key]
		if exists && fp.ContentHash == hash {
			result.Unchanged = append(result.Unchanged, RegionFingerprint{RegionID: regionID, ContentHash: hash})
		} else {
			result.ChangedRegions = append(result.ChangedRegions, RegionFingerprint{RegionID: regionID, ContentHash: hash})
		}
		e.fingerprints[key] = RegionFingerprint{RegionID: regionID, ContentHash: hash, LastSeenAt: time.Now().Unix()}
	}
	return result
}

// Is304 checks if a response status indicates unmodified content.
func Is304(status int) bool { return status == http.StatusNotModified }

// Apply304 marks a diff result as served from conditional cache.
func (e *DiffEngine) Apply304(url string) DiffResult {
	return DiffResult{URL: url, Conditional304: true}
}

// Prune removes fingerprints older than maxAge.
func (e *DiffEngine) Prune(maxAge time.Duration) int {
	now := time.Now().Unix()
	maxSec := int64(maxAge.Seconds())
	removed := 0
	for k, fp := range e.fingerprints {
		if now-fp.LastSeenAt > maxSec {
			delete(e.fingerprints, k)
			removed++
		}
	}
	return removed
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
