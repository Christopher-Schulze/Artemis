// Package diagnostics owns Artemis' redacted, retention-bounded audit ledger.
package diagnostics

import (
	"bufio"
	"bytes"
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
}

var tokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,127}$`)
var sessionRefPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

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
		return store, nil
	}
	store.mu.Lock()
	err := store.withFileLock(func() error {
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
	return filepath.Join(home, ".omnimus", "audit", "artemis.jsonl"), nil
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

func HashSession(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("artemis-session:" + sessionID))
	return hex.EncodeToString(sum[:16])
}

func (s *Store) AppendPolicy(decision PolicyDecision) error {
	if decision.Operation == "" {
		decision.Operation = "unknown"
	}
	if decision.Transport == "" {
		decision.Transport = "unknown"
	}
	if decision.ReasonCode == "" {
		decision.ReasonCode = "unspecified"
	}
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
	if !tokenPattern.MatchString(decision.Operation) || !tokenPattern.MatchString(decision.Transport) || !tokenPattern.MatchString(decision.ReasonCode) {
		return errors.New("diagnostics policy tokens invalid")
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
	if !tokenPattern.MatchString(usage.Scope) {
		return errors.New("diagnostics resource scope invalid")
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
	info, err := os.Stat(s.config.Path)
	if errors.Is(err, os.ErrNotExist) {
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
	if info.Size() == s.fileSize && info.ModTime().Equal(s.fileModTime) {
		return s.pruneLocked(now), nil
	}
	return s.loadFileLocked(now)
}

func (s *Store) loadFileLocked(now time.Time) (changed bool, resultErr error) {
	file, err := os.Open(s.config.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.records = nil
		s.bytes = 0
		s.fileSize = 0
		s.fileModTime = time.Time{}
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
	if info.Size() > s.config.MaxBytes+int64(s.config.MaxRecordBytes) {
		return false, fmt.Errorf("diagnostics ledger exceeds recoverable size bound")
	}
	var records []Record
	var total int64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), s.config.MaxRecordBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
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
		if err := validateRecord(record); err != nil {
			return false, err
		}
		records = append(records, record)
		total += int64(len(line) + 1)
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("diagnostics scan: %w", err)
	}
	s.records = records
	s.bytes = total
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
	return s.pruneLocked(now), nil
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
	file, err := os.OpenFile(s.config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
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
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
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
	info, err := os.Stat(s.config.Path)
	if err != nil {
		return fmt.Errorf("diagnostics stat after replace: %w", err)
	}
	s.fileSize = info.Size()
	s.fileModTime = info.ModTime()
	return nil
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
