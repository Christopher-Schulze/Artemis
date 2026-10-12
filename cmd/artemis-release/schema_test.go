package main

import (
	"strings"
	"testing"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

func TestCycloneDXSchemaRejectsInvalidDocuments(t *testing.T) {
	for _, document := range []string{
		`{}`,
		`{"bomFormat":"CycloneDX","specVersion":"1.7","unexpected":true}`,
		`{"bomFormat":"Other","specVersion":"1.7"}`,
	} {
		if err := validateCycloneDXSchema([]byte(document)); err == nil {
			t.Fatalf("invalid CycloneDX document passed schema validation: %s", document)
		}
	}
}

func TestValidateUniqueBOMRefsRejectsDuplicates(t *testing.T) {
	components := []cdx.Component{{Name: "dependency", BOMRef: "duplicate"}}
	tools := []cdx.Component{{Name: "generator", BOMRef: "tool"}}
	bom := cdx.BOM{
		Metadata:   &cdx.Metadata{Component: &cdx.Component{Name: "root", BOMRef: "duplicate"}, Tools: &cdx.ToolsChoice{Components: &tools}},
		Components: &components,
	}
	if err := validateUniqueBOMRefs(bom); err == nil {
		t.Fatal("duplicate CycloneDX bom-ref passed validation")
	}
}

func TestEmbeddedCycloneDXSchemaPin(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		digest string
	}{
		{name: "CycloneDX 1.7", data: cyclonedx17Schema, digest: "6166dcd7ab0176139587650dee1427972122ac488b83c34f39933966b6146ffe"},
		{name: "SPDX", data: cyclonedxSPDXSchema, digest: "669315cee4265ad6d742e92d7afb062fb4847abe754d3ade544b7c40482d0a48"},
		{name: "JSF", data: cyclonedxJSFSchema, digest: "a517f9e483e2252debd23d5c62edb72805b6d3932935a6ebb8196745052d2dc3"},
		{name: "cryptography definitions", data: cyclonedxCryptoSchema, digest: "9f7bdac04dc965aaa2f15076d693d98178625544d9ea81bb5c644e5ac17cc089"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.TrimSpace(test.data) == "" || stableDigestBytes([]byte(test.data)) != test.digest {
				t.Fatalf("embedded schema %s drifted", test.name)
			}
		})
	}
}
