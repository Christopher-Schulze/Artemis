// Package diagnostics owns Artemis' redacted, retention-bounded audit ledger.
package diagnostics

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxRecords           = 4096
	DefaultMaxBytes       int64 = 8 << 20
	DefaultMaxAge               = 7 * 24 * time.Hour
	DefaultMaxRecordBytes       = 16 << 10
)

type RecordType string

const (
	RecordPolicyDecision RecordType = "policy_decision"
	RecordResourceUsage  RecordType = "resource_usage"
)

type PolicyDecision struct {
	Operation  string `json:"operation"`
	Transport  string `json:"transport"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Result     string `json:"result"`
	ReasonCode string `json:"reason_code"`
	SessionRef string `json:"session_ref,omitempty"`
}

type ResourceUsage struct {
	Scope          string  `json:"scope"`
	SessionRef     string  `json:"session_ref,omitempty"`
	CPUPercent     float64 `json:"cpu_percent,omitempty"`
	MemoryBytes    int64   `json:"memory_bytes,omitempty"`
	Requests       int     `json:"requests,omitempty"`
	ResponseBytes  int64   `json:"response_bytes,omitempty"`
	DiskBytes      int64   `json:"disk_bytes,omitempty"`
	ActiveTabs     int     `json:"active_tabs,omitempty"`
	Concurrent     int     `json:"concurrent,omitempty"`
	SessionStopped bool    `json:"session_stopped,omitempty"`
}

type Record struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      RecordType      `json:"type"`
	Policy    *PolicyDecision `json:"policy,omitempty"`
	Resource  *ResourceUsage  `json:"resource,omitempty"`
}

type Config struct {
	Path           string           `json:"path,omitempty"`
	MaxRecords     int              `json:"max_records,omitempty"`
	MaxBytes       int64            `json:"max_bytes,omitempty"`
	MaxAge         time.Duration    `json:"max_age,omitempty"`
	MaxRecordBytes int              `json:"max_record_bytes,omitempty"`
	Now            func() time.Time `json:"-"`
}

type Store struct {
	mu          sync.Mutex
	config      Config
	records     []Record
	bytes       int64
	fileSize    int64
	fileModTime time.Time
	fileInfo    os.FileInfo
	sessionKey  []byte
}

var sessionRefPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Closed value sets: diagnostics never retain caller-chosen strings, so a
// token-shaped secret cannot enter the ledger through a reason or scope.
var (
	policyOperations = closedSet("navigation", "redirect", "subframe", "worker", "subresource", "websocket", "download", "proxy", "socket", "unknown")
	policyTransports = closedSet("http", "https", "ws", "wss", "unknown", "other")
	policyReasons    = closedSet("policy_match", "policy_match_proxied", "content_type_not_allowed", "dns_failed", "download_content_type_not_allowed", "download_too_large", "host_not_allowed", "invalid_download_size", "invalid_response_encoding", "invalid_target_kind", "invalid_url", "method_not_allowed", "non_destination_address", "non_public_address", "port_not_allowed", "request_body_length_unknown", "request_body_too_large", "response_body_too_large", "scheme_not_allowed", "unspecified", "other")
	resourceScopes   = closedSet("renderless", "chromium")
)

func closedSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// closedValue maps a producer value onto its closed set: empty becomes
// empty (caller default), unknown values become fallback.
func closedValue(set map[string]bool, value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if set[value] {
		return value
	}
	return fallback
}

func NewStore(config Config) (*Store, error) {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Path != "" {
		path, err := filepath.Abs(config.Path)
		if err != nil {
			return nil, fmt.Errorf("diagnostics path: %w", err)
		}
		config.Path = path
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("diagnostics directory: %w", err)
		}
	}
	store := &Store{config: config}
	if config.Path == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("diagnostics session key: %w", err)
		}
		store.sessionKey = key
		return store, nil
	}
	if err := ensurePrivateStore(config.Path); err != nil {
		return nil, err
	}
	key, err := loadSessionKey(filepath.Join(filepath.Dir(config.Path), "session.key"))
	if err != nil {
		return nil, err
	}
	store.sessionKey = key
	store.mu.Lock()
	err = store.withFileLock(func() error {
		changed, loadErr := store.loadFileLocked(config.Now())
		if loadErr != nil {
			return loadErr
		}
		if changed {
			return store.rewriteLocked()
		}
		return nil
	})
	store.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return store, nil
}

func DefaultFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("diagnostics home: %w", err)
	}
	return filepath.Join(home, ".artemis", "audit", "artemis.jsonl"), nil
}

func DefaultPersistentConfig() (Config, error) {
	path := strings.TrimSpace(os.Getenv("ARTEMIS_DIAGNOSTICS_FILE"))
	if path == "" {
		var err error
		path, err = DefaultFilePath()
		if err != nil {
			return Config{}, err
		}
	}
	return normalizeConfig(Config{Path: path}), nil
}

// SessionRef is a pseudonymous session reference keyed per installation
// (persistent store: <dir>/session.key) or per process (memory store), so the
// same session ID is not linkable across installations.
func (s *Store) SessionRef(sessionID string) string {
	if s == nil || sessionID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.sessionKey)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

func normalizePolicy(decision PolicyDecision) PolicyDecision {
	decision.Operation = closedValue(policyOperations, decision.Operation, "unknown")
	if strings.TrimSpace(decision.Transport) == "" {
		decision.Transport = "unknown"
	}
	decision.Transport = closedValue(policyTransports, decision.Transport, "other")
	if strings.TrimSpace(decision.ReasonCode) == "" {
		decision.ReasonCode = "unspecified"
	}
	decision.ReasonCode = closedValue(policyReasons, decision.ReasonCode, "other")
	return decision
}

func (s *Store) AppendPolicy(decision PolicyDecision) error {
	decision = normalizePolicy(decision)
	return s.append(Record{Timestamp: s.config.Now().UTC(), Type: RecordPolicyDecision, Policy: &decision})
}

func (s *Store) AppendResource(usage ResourceUsage) error {
	return s.append(Record{Timestamp: s.config.Now().UTC(), Type: RecordResourceUsage, Resource: &usage})
}

func (s *Store) Snapshot() ([]Record, error) {
	if s == nil {
		return nil, errors.New("diagnostics store required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Path != "" {
		if err := s.withFileLock(func() error {
			changed, err := s.refreshFileLocked(s.config.Now())
			if err != nil {
				return err
			}
			if changed {
				return s.rewriteLocked()
			}
			return nil
		}); err != nil {
			return nil, err
		}
	} else {
		s.pruneLocked(s.config.Now())
	}
	return cloneRecords(s.records), nil
}

func (s *Store) append(record Record) error {
	if s == nil {
		return errors.New("diagnostics store required")
	}
	if err := validateRecord(record); err != nil {
		return err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("diagnostics encode: %w", err)
	}
	if len(line)+1 > s.config.MaxRecordBytes || int64(len(line)+1) > s.config.MaxBytes {
		return fmt.Errorf("diagnostics record exceeds %d bytes", s.config.MaxRecordBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	write := func() error {
		refreshPruned := false
		if s.config.Path != "" {
			var err error
			refreshPruned, err = s.refreshFileLocked(s.config.Now())
			if err != nil {
				return err
			}
		}
		previousRecords := cloneRecords(s.records)
		previousBytes := s.bytes
		s.records = append(s.records, record)
		s.bytes += int64(len(line) + 1)
		pruned := s.pruneLocked(s.config.Now())
		if s.config.Path == "" {
			return nil
		}
		var persistErr error
		if refreshPruned || pruned {
			persistErr = s.rewriteLocked()
		} else {
			persistErr = s.appendLineLocked(line)
		}
		if persistErr != nil {
			s.records = previousRecords
			s.bytes = previousBytes
			s.fileSize = -1
			s.fileModTime = time.Time{}
		}
		return persistErr
	}
	if s.config.Path == "" {
		return write()
	}
	return s.withFileLock(write)
}

func normalizeConfig(config Config) Config {
	if config.MaxRecords == 0 {
		config.MaxRecords = DefaultMaxRecords
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = DefaultMaxBytes
	}
	if config.MaxAge == 0 {
		config.MaxAge = DefaultMaxAge
	}
	if config.MaxRecordBytes == 0 {
		config.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config
}

func validateConfig(config Config) error {
	if config.MaxRecords < 1 || config.MaxBytes < 1 || config.MaxAge < time.Second || config.MaxRecordBytes < 256 {
		return errors.New("diagnostics retention limits must be positive and max age must be at least one second")
	}
	if int64(config.MaxRecordBytes) > config.MaxBytes {
		return errors.New("diagnostics max record bytes exceeds ledger byte limit")
	}
	return nil
}

func validateRecord(record Record) error {
	if record.Timestamp.IsZero() {
		return errors.New("diagnostics timestamp required")
	}
	switch record.Type {
	case RecordPolicyDecision:
		if record.Policy == nil || record.Resource != nil {
			return errors.New("diagnostics policy record shape invalid")
		}
		return validatePolicy(*record.Policy)
	case RecordResourceUsage:
		if record.Resource == nil || record.Policy != nil {
			return errors.New("diagnostics resource record shape invalid")
		}
		return validateResource(*record.Resource)
	default:
		return fmt.Errorf("diagnostics record type %q invalid", record.Type)
	}
}

func validatePolicy(decision PolicyDecision) error {
	if !policyOperations[decision.Operation] || !policyTransports[decision.Transport] || !policyReasons[decision.ReasonCode] {
		return errors.New("diagnostics policy values outside the closed sets")
	}
	if decision.Result != "allow" && decision.Result != "deny" {
		return errors.New("diagnostics policy result invalid")
	}
	if decision.Port < 0 || decision.Port > 65535 {
		return errors.New("diagnostics policy port invalid")
	}
	if len(decision.Host) > 253 || strings.ContainsAny(decision.Host, "/?#@\\") {
		return errors.New("diagnostics policy host is not redacted")
	}
	for _, char := range decision.Host {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune(".-:", char) {
			continue
		}
		return errors.New("diagnostics policy host is not canonical ASCII")
	}
	return validateSessionRef(decision.SessionRef)
}

func validateResource(usage ResourceUsage) error {
	if !resourceScopes[usage.Scope] {
		return errors.New("diagnostics resource scope outside the closed set")
	}
	if math.IsNaN(usage.CPUPercent) || math.IsInf(usage.CPUPercent, 0) || usage.CPUPercent < 0 || usage.MemoryBytes < 0 || usage.Requests < 0 || usage.ResponseBytes < 0 || usage.DiskBytes < 0 || usage.ActiveTabs < 0 || usage.Concurrent < 0 {
		return errors.New("diagnostics resource values must be finite and non-negative")
	}
	return validateSessionRef(usage.SessionRef)
}

func validateSessionRef(ref string) error {
	if ref != "" && !sessionRefPattern.MatchString(ref) {
		return errors.New("diagnostics session reference invalid")
	}
	return nil
}

func (s *Store) refreshFileLocked(now time.Time) (bool, error) {
	info, err := os.Lstat(s.config.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.fileInfo = nil
		if s.fileSize == 0 && len(s.records) == 0 {
			return false, nil
		}
		s.records = nil
		s.bytes = 0
		s.fileSize = 0
		s.fileModTime = time.Time{}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("diagnostics stat: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("diagnostics ledger is not a regular file")
	}
	if s.fileInfo != nil && os.SameFile(s.fileInfo, info) && info.Size() == s.fileSize && info.ModTime().Equal(s.fileModTime) {
		return s.pruneLocked(now), nil
	}
	return s.loadFileLocked(now)
}

func (s *Store) loadFileLocked(now time.Time) (changed bool, resultErr error) {
	file, err := os.OpenFile(s.config.Path, os.O_RDONLY|openNoFollow, 0)
	if errors.Is(err, os.ErrNotExist) {
		s.records = nil
		s.bytes = 0
		s.fileSize = 0
		s.fileModTime = time.Time{}
		s.fileInfo = nil
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("diagnostics open: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("diagnostics close: %w", closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("diagnostics stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("diagnostics ledger is not a regular file")
	}
	if info.Size() > s.config.MaxBytes+int64(s.config.MaxRecordBytes) {
		return false, fmt.Errorf("diagnostics ledger exceeds recoverable size bound")
	}
	content, err := io.ReadAll(io.LimitReader(file, s.config.MaxBytes+int64(s.config.MaxRecordBytes)+1))
	if err != nil {
		return false, fmt.Errorf("diagnostics read: %w", err)
	}
	lines := bytes.Split(content, []byte{'\n'})
	// Every acknowledged record ends with a newline; an unterminated tail is
	// an append torn by a crash and is dropped instead of poisoning the store.
	torn := len(lines[len(lines)-1]) > 0
	lines = lines[:len(lines)-1]
	var records []Record
	var total int64
	for _, raw := range lines {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if len(line) > s.config.MaxRecordBytes {
			return false, errors.New("diagnostics record exceeds the record size bound")
		}
		var record Record
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return false, fmt.Errorf("diagnostics decode: %w", err)
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return false, errors.New("diagnostics record has trailing data")
		}
		if record.Policy != nil {
			normalized := normalizePolicy(*record.Policy)
			record.Policy = &normalized
		}
		if err := validateRecord(record); err != nil {
			return false, err
		}
		records = append(records, record)
		total += int64(len(line) + 1)
	}
	s.records = records
	s.bytes = total
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
	s.fileInfo = info
	return s.pruneLocked(now) || torn, nil
}

func (s *Store) pruneLocked(now time.Time) bool {
	cutoff := now.UTC().Add(-s.config.MaxAge)
	kept := s.records[:0]
	var total int64
	changed := false
	for _, record := range s.records {
		if record.Timestamp.Before(cutoff) {
			changed = true
			continue
		}
		kept = append(kept, record)
		total += recordSize(record)
	}
	s.records = kept
	s.bytes = total
	for len(s.records) > s.config.MaxRecords || s.bytes > s.config.MaxBytes {
		s.bytes -= recordSize(s.records[0])
		s.records = s.records[1:]
		changed = true
	}
	return changed
}

func recordSize(record Record) int64 {
	encoded, err := json.Marshal(record)
	if err != nil {
		return 0
	}
	return int64(len(encoded) + 1)
}

func (s *Store) appendLineLocked(line []byte) error {
	created := s.fileInfo == nil
	file, err := os.OpenFile(s.config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|openNoFollow, 0o600)
	if err != nil {
		return fmt.Errorf("diagnostics append: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("diagnostics permissions: %w", err)
	}
	payload := append(append([]byte(nil), line...), '\n')
	written, writeErr := file.Write(payload)
	if writeErr == nil && written != len(payload) {
		writeErr = io.ErrShortWrite
	}
	var syncErr error
	var info os.FileInfo
	var statErr error
	if writeErr == nil {
		syncErr = file.Sync()
	}
	if writeErr == nil && syncErr == nil {
		info, statErr = file.Stat()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("diagnostics write: %w", writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("diagnostics sync: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("diagnostics close: %w", closeErr)
	}
	if statErr != nil {
		return fmt.Errorf("diagnostics stat after append: %w", statErr)
	}
	if created {
		if err := syncDirectory(filepath.Dir(s.config.Path)); err != nil {
			return err
		}
	}
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
	s.fileInfo = info
	return nil
}

func (s *Store) rewriteLocked() error {
	dir := filepath.Dir(s.config.Path)
	temporary, err := os.CreateTemp(dir, ".artemis-diagnostics-*.tmp")
	if err != nil {
		return fmt.Errorf("diagnostics temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if chmodErr := temporary.Chmod(0o600); chmodErr != nil {
		return fmt.Errorf("diagnostics temporary permissions: %w", chmodErr)
	}
	writer := bufio.NewWriter(temporary)
	for _, record := range s.records {
		line, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			return fmt.Errorf("diagnostics encode: %w", marshalErr)
		}
		if _, writeErr := writer.Write(append(line, '\n')); writeErr != nil {
			return fmt.Errorf("diagnostics rewrite: %w", writeErr)
		}
	}
	if flushErr := writer.Flush(); flushErr != nil {
		return fmt.Errorf("diagnostics flush: %w", flushErr)
	}
	if syncErr := temporary.Sync(); syncErr != nil {
		return fmt.Errorf("diagnostics sync: %w", syncErr)
	}
	if closeErr := temporary.Close(); closeErr != nil {
		return fmt.Errorf("diagnostics close temporary: %w", closeErr)
	}
	if renameErr := os.Rename(temporaryPath, s.config.Path); renameErr != nil {
		return fmt.Errorf("diagnostics replace: %w", renameErr)
	}
	committed = true
	if err := syncDirectory(dir); err != nil {
		return err
	}
	info, err := os.Lstat(s.config.Path)
	if err != nil {
		return fmt.Errorf("diagnostics stat after replace: %w", err)
	}
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
	s.fileInfo = info
	return nil
}

// syncDirectory makes a file creation or rename in dir durable.
func syncDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("diagnostics directory sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("diagnostics directory sync: %w", err)
	}
	return nil
}

// ensurePrivateStore requires the ledger directory to be a real private
// directory and the ledger, if present, a regular non-symlink file.
func ensurePrivateStore(path string) error {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("diagnostics directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("diagnostics directory must be a real directory, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("diagnostics directory permissions: %w", err)
		}
	}
	for _, candidate := range []string{path, path + ".lock", filepath.Join(dir, "session.key")} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("diagnostics path: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("diagnostics path %s must be a regular file, not a symlink", filepath.Base(candidate))
		}
	}
	return nil
}

// loadSessionKey reads or creates the installation-scoped pseudonym key.
func loadSessionKey(path string) ([]byte, error) {
	key, err := readSessionKey(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("diagnostics session key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readSessionKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("diagnostics session key: %w", err)
	}
	_, writeErr := file.Write(key)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, fmt.Errorf("diagnostics session key: %w", err)
	}
	return key, syncDirectory(filepath.Dir(path))
}

func readSessionKey(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, fmt.Errorf("diagnostics session key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("diagnostics session key is corrupt")
	}
	return key, nil
}

func (s *Store) withFileLock(action func() error) error {
	lock, err := acquireFileLock(s.config.Path + ".lock")
	if err != nil {
		return err
	}
	defer lock.release()
	return action()
}

func cloneRecords(records []Record) []Record {
	result := make([]Record, len(records))
	for i, record := range records {
		result[i] = record
		if record.Policy != nil {
			policy := *record.Policy
			result[i].Policy = &policy
		}
		if record.Resource != nil {
			resource := *record.Resource
			result[i].Resource = &resource
		}
	}
	return result
}
