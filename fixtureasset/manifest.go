// Package fixtureasset defines portable bounded fixture manifest transport.
// It owns no registry, storage, policy or execution authority.
package fixtureasset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxFixtureAssetBytes bounds bytes supplied to manifest admission.
const MaxFixtureAssetBytes = 16 << 20
const maxFixtureManifestBytes = 128 << 10

// Kind identifies the closed proof-asset families accepted by the transport.
type Kind string

const (
	KindBenchmark          Kind = "benchmark"
	KindCustomerAcceptance Kind = "customer_acceptance"
	KindParser             Kind = "parser"
	KindOCR                Kind = "ocr"
	KindConnector          Kind = "connector"
	KindUIFlow             Kind = "ui_flow"
	KindReplay             Kind = "replay"
	KindProductionTrace    Kind = "production_trace"
	KindPolicyScenario     Kind = "policy_scenario"
	KindMigration          Kind = "migration"
	KindDoctor             Kind = "doctor"
	KindPackValidation     Kind = "pack_validation"
)

// PrivacyClass carries a declared content class, not a readership grant.
type PrivacyClass string

const (
	PrivacyPublic          PrivacyClass = "public"
	PrivacyInternal        PrivacyClass = "internal"
	PrivacyPersonal        PrivacyClass = "personal"
	PrivacySpecialCategory PrivacyClass = "special_category"
)

// ReplayMode declares the intended execution mode without authorizing it.
type ReplayMode string

const (
	ReplayIsolated ReplayMode = "isolated"
	ReplayShadow   ReplayMode = "shadow"
	ReplayDryRun   ReplayMode = "dry_run"
)

// Manifest describes portable proof input. It grants no permission,
// producer authenticity, retention disposition or passing-coverage claim.
type Manifest struct {
	ID                   string       `json:"id"`
	OwnerSubsystem       string       `json:"owner_subsystem"`
	AssetPath            string       `json:"asset_path"`
	AssetHash            string       `json:"asset_hash"`
	FixtureKind          Kind         `json:"fixture_kind"`
	PrivacyClass         PrivacyClass `json:"privacy_class"`
	DomainRefs           []string     `json:"domain_refs"`
	CapabilityRefs       []string     `json:"capability_refs"`
	ConnectorRefs        []string     `json:"connector_refs"`
	ExpectedArtifactRefs []string     `json:"expected_artifact_refs"`
	ReplayMode           ReplayMode   `json:"replay_mode"`
	RunnerRef            string       `json:"runner_ref"`
	QualityGateRef       string       `json:"quality_gate_ref"`
	Retention            string       `json:"retention"`
}

// Validate binds the declared asset digest to supplied bytes. AssetPath and
// other references remain opaque; their permissioned resolution belongs to
// the embedding authority, not this transport contract.
func (m Manifest) Validate(content []byte) error {
	if len(content) > MaxFixtureAssetBytes {
		return fmt.Errorf("fixture manifest: asset byte limit exceeded")
	}
	if err := m.validateMetadata(); err != nil {
		return err
	}
	hash := sha256.Sum256(content)
	if m.AssetHash != hex.EncodeToString(hash[:]) {
		return fmt.Errorf("fixture manifest: actual asset hash differs")
	}
	return nil
}

// ValidateReference checks bounded inert metadata and a canonical SHA-256
// reference. It does not read or verify source bytes or grant any authority.
func (m Manifest) ValidateReference() error {
	if err := m.validateMetadata(); err != nil {
		return err
	}
	if len(m.AssetHash) != sha256.Size*2 {
		return fmt.Errorf("fixture manifest: canonical SHA-256 reference required")
	}
	decoded, err := hex.DecodeString(m.AssetHash)
	if err != nil || hex.EncodeToString(decoded) != m.AssetHash {
		return fmt.Errorf("fixture manifest: canonical SHA-256 reference required")
	}
	return nil
}

func (m Manifest) validateMetadata() error {
	fields := []string{m.ID, m.OwnerSubsystem, m.AssetHash, string(m.FixtureKind), string(m.PrivacyClass), string(m.ReplayMode), m.RunnerRef, m.QualityGateRef, m.Retention}
	total := len(m.AssetPath)
	if err := validateFixtureText(m.AssetPath, 1024); err != nil {
		return err
	}
	for _, field := range fields {
		if err := validateFixtureText(field, 256); err != nil {
			return err
		}
		total += len(field)
	}
	if err := m.validateValues(); err != nil {
		return err
	}
	for _, refs := range [][]string{m.DomainRefs, m.CapabilityRefs, m.ConnectorRefs, m.ExpectedArtifactRefs} {
		if err := validateFixtureRefs(refs, &total); err != nil {
			return err
		}
	}
	if total > 32<<10 {
		return fmt.Errorf("fixture manifest: metadata byte limit exceeded")
	}
	return nil
}

func (m Manifest) validateValues() error {
	switch m.FixtureKind {
	case KindBenchmark, KindCustomerAcceptance, KindParser, KindOCR, KindConnector, KindUIFlow, KindReplay, KindProductionTrace, KindPolicyScenario, KindMigration, KindDoctor, KindPackValidation:
	default:
		return fmt.Errorf("fixture manifest: unsupported fixture kind")
	}
	switch m.PrivacyClass {
	case PrivacyPublic, PrivacyInternal, PrivacyPersonal, PrivacySpecialCategory:
	default:
		return fmt.Errorf("fixture manifest: unknown content privacy class")
	}
	switch m.ReplayMode {
	case ReplayIsolated, ReplayShadow, ReplayDryRun:
	default:
		return fmt.Errorf("fixture manifest: unknown replay mode")
	}
	return nil
}

func validateFixtureText(value string, limit int) error {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return fmt.Errorf("fixture manifest: bounded nonempty UTF-8 reference required")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("fixture manifest: control character in reference")
		}
	}
	return nil
}

func validateFixtureRefs(refs []string, total *int) error {
	if len(refs) > 64 {
		return fmt.Errorf("fixture manifest: reference count limit exceeded")
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if err := validateFixtureText(ref, 256); err != nil {
			return err
		}
		if seen[ref] {
			return fmt.Errorf("fixture manifest: duplicate reference")
		}
		seen[ref] = true
		*total += len(ref)
	}
	return nil
}

// Detached returns independent reference lists without changing their order.
func (m Manifest) Detached() Manifest {
	m.DomainRefs = append([]string{}, m.DomainRefs...)
	m.CapabilityRefs = append([]string{}, m.CapabilityRefs...)
	m.ConnectorRefs = append([]string{}, m.ConnectorRefs...)
	m.ExpectedArtifactRefs = append([]string{}, m.ExpectedArtifactRefs...)
	return m
}

// Encode emits validated canonical JSON with all fields.
func Encode(manifest Manifest, content []byte) ([]byte, error) {
	if err := manifest.Validate(content); err != nil {
		return nil, err
	}
	data, err := json.Marshal(manifest.Detached())
	if err != nil {
		return nil, err
	}
	if len(data) > maxFixtureManifestBytes {
		return nil, fmt.Errorf("fixture manifest: encoded byte limit exceeded")
	}
	return data, nil
}

// Decode rejects unknown/duplicate fields, omitted/null
// reference lists, trailing data and noncanonical JSON before asset admission.
func Decode(data, content []byte) (Manifest, error) {
	if len(data) > maxFixtureManifestBytes || !utf8.Valid(data) {
		return Manifest{}, fmt.Errorf("fixture manifest: encoded byte/UTF-8 limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Manifest{}, fmt.Errorf("fixture manifest: trailing data")
	}
	canonical, err := Encode(manifest, content)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Manifest{}, fmt.Errorf("fixture manifest: noncanonical or incomplete JSON")
	}
	return manifest.Detached(), nil
}
