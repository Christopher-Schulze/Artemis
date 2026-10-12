package js

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const SnapshotManifestSchemaVersion = 1

// SnapshotSourceDigest is the immutable identity of one snapshot-eligible
// bootstrap source. The order in SnapshotInputManifest.BootstrapSources is
// part of the snapshot contract.
type SnapshotSourceDigest struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// SnapshotInputManifest is the canonical, deterministic input projection for
// the V8 startup snapshot generator.
type SnapshotInputManifest struct {
	SchemaVersion    int                    `json:"schema_version"`
	StubSHA256       string                 `json:"stub_sha256"`
	BootstrapSources []SnapshotSourceDigest `json:"bootstrap_sources"`
	NativeStubNames  []string               `json:"native_stub_names"`
	SourceSetSHA256  string                 `json:"source_set_sha256"`
}

var snapshotNativeStubNames = []string{
	"__append_child", "__attr_get", "__attr_keys", "__attr_remove", "__attr_set",
	"__bb_get", "__cancel_animation_frame", "__cascade_style", "__clone_node",
	"__close_event", "__console", "__cookie_get", "__cookie_set", "__create_comment",
	"__create_element", "__create_event", "__create_event_target", "__create_text",
	"__crypto_aes_decrypt", "__crypto_aes_encrypt", "__crypto_aes_kw_unwrap",
	"__crypto_aes_kw_wrap", "__crypto_complete", "__crypto_derive_aes",
	"__crypto_derive_bits", "__crypto_derive_hmac", "__crypto_digest",
	"__crypto_ecdh_derive", "__crypto_ecdh_generate", "__crypto_ecdsa_generate",
	"__crypto_ecdsa_sign", "__crypto_ecdsa_verify", "__crypto_export_jwk",
	"__crypto_export_pkcs8", "__crypto_export_raw", "__crypto_export_spki",
	"__crypto_extra", "__crypto_generate_aes", "__crypto_generate_hmac",
	"__crypto_get_random", "__crypto_hkdf", "__crypto_hmac", "__crypto_import_jwk",
	"__crypto_import_pkcs8", "__crypto_import_raw", "__crypto_import_spki",
	"__crypto_pbkdf2", "__crypto_pkcs8", "__crypto_random", "__crypto_random_uuid",
	"__crypto_rsa_decrypt", "__crypto_rsa_encrypt", "__crypto_rsa_generate",
	"__crypto_rsa_sign", "__crypto_rsa_verify", "__crypto_subtle", "__crypto_uuid",
	"__cssom_get", "__cssom_set", "__data_get", "__data_set", "__dispatch_event",
	"__document", "__dom_token_list", "__el_get_input_props", "__el_get_input_value",
	"__el_set_input_value", "__fetch", "__fetch_abort", "__fetch_async",
	"__file_array_buffer", "__file_text", "__form_data", "__form_get", "__form_set",
	"__form_submit", "__formdata_append", "__formdata_get", "__formdata_pairs",
	"__get_attr", "__get_attr_keys", "__get_children", "__get_html", "__get_inner_text",
	"__get_node", "__get_outer_html", "__get_parent", "__get_sibling", "__get_text",
	"__headers_get", "__headers_init", "__html_decode", "__html_encode",
	"__iframe_get_doc", "__iframe_load", "__iframe_post", "__input_get", "__input_props",
	"__input_set", "__insert_before", "__list_on_attrs", "__list_props", "__location",
	"__mkblob", "__mkfile", "__mkresp", "__mutation_observer", "__node_at", "__node_clear",
	"__node_count", "__node_in_doc", "__node_lookup", "__node_owner", "__observer_disconnect",
	"__observer_register", "__observer_take", "__open_window", "__performance_now",
	"__post_message", "__query_selector", "__query_selector_all", "__readable_pull",
	"__reject_promise", "__remove_attr", "__remove_child", "__replace_child",
	"__request_animation_frame", "__request_init", "__resolve_promise", "__resp_blob",
	"__resp_clone", "__resp_init", "__resp_text", "__schedule_microtask", "__set_attr",
	"__set_html", "__set_inner_text", "__set_text", "__storage_clear", "__storage_get",
	"__storage_key", "__storage_length", "__storage_remove", "__storage_set", "__style_compute",
	"__style_get", "__style_set", "__submit_form", "__tagname", "__text_decode", "__text_encode",
	"__url", "__url_search", "__wrap", "__ws_close", "__ws_drain", "__ws_open", "__ws_send",
}

var snapshotNativeNamePattern = regexp.MustCompile(`"(__[A-Za-z][A-Za-z0-9_]*)"`)

// The callback-name accessor returns a defensive copy of the canonical native
// inventory used by the snapshot bootstrap.
func SnapshotNativeStubNames() []string {
	return append([]string(nil), snapshotNativeStubNames...)
}

// BuildSnapshotInputManifest derives the complete ordered snapshot input
// identity. The bootstrap prelude source is included so changes to the prelude or
// generated callback inventory cannot leave a stale blob accepted.
func BuildSnapshotInputManifest(stubSource string) (SnapshotInputManifest, error) {
	if strings.TrimSpace(stubSource) == "" {
		return SnapshotInputManifest{}, errors.New("snapshot input: empty stub source")
	}
	if err := validateNativeStubInventory(stubSource); err != nil {
		return SnapshotInputManifest{}, err
	}

	sources := BootstrapSources()
	seen := make(map[string]struct{}, len(sources))
	digests := make([]SnapshotSourceDigest, 0, len(sources))
	for index, source := range sources {
		if strings.TrimSpace(source.Name) == "" || source.Source == "" {
			return SnapshotInputManifest{}, fmt.Errorf("snapshot input: bootstrap %d is incomplete", index)
		}
		if _, ok := seen[source.Name]; ok {
			return SnapshotInputManifest{}, fmt.Errorf("snapshot input: duplicate bootstrap %q", source.Name)
		}
		seen[source.Name] = struct{}{}
		digests = append(digests, SnapshotSourceDigest{
			Name:   source.Name,
			Size:   len(source.Source),
			SHA256: digestString([]byte(source.Source)),
		})
	}

	names := SnapshotNativeStubNames()
	canonical := strings.Builder{}
	fmt.Fprintf(&canonical, "schema_version=%d\nstub_sha256=%s\n", SnapshotManifestSchemaVersion, digestString([]byte(stubSource)))
	for index, source := range digests {
		fmt.Fprintf(&canonical, "bootstrap[%d].name=%s\nbootstrap[%d].size=%d\nbootstrap[%d].sha256=%s\n", index, source.Name, index, source.Size, index, source.SHA256)
	}
	for index, name := range names {
		fmt.Fprintf(&canonical, "native_stub[%d]=%s\n", index, name)
	}

	return SnapshotInputManifest{
		SchemaVersion:    SnapshotManifestSchemaVersion,
		StubSHA256:       digestString([]byte(stubSource)),
		BootstrapSources: digests,
		NativeStubNames:  names,
		SourceSetSHA256:  digestString([]byte(canonical.String())),
	}, nil
}

func validateNativeStubInventory(source string) error {
	actualMatches := snapshotNativeNamePattern.FindAllStringSubmatch(source, -1)
	actual := make([]string, 0, len(actualMatches))
	seen := make(map[string]struct{}, len(actualMatches))
	for _, match := range actualMatches {
		name := match[1]
		if _, ok := seen[name]; ok {
			return fmt.Errorf("snapshot input: duplicate native stub %q", name)
		}
		seen[name] = struct{}{}
		actual = append(actual, name)
	}
	want := SnapshotNativeStubNames()
	sort.Strings(actual)
	sort.Strings(want)
	if len(actual) != len(want) {
		return fmt.Errorf("snapshot input: native stub inventory count=%d, want=%d", len(actual), len(want))
	}
	for index := range want {
		if actual[index] != want[index] {
			return fmt.Errorf("snapshot input: native stub inventory mismatch at %d: got %q, want %q", index, actual[index], want[index])
		}
	}
	return nil
}

func digestString(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
