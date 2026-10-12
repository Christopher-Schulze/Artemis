package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

func writeSBOM(path string, inputs releaseInputs, inventory moduleInventory, report licenseReport, writeFile func(string, []byte, os.FileMode) error) error {
	data, err := encodeSBOM(inputs, inventory, report)
	if err != nil {
		return err
	}
	if err = validateCycloneDXSchema(data); err != nil {
		return err
	}
	if err := writeFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write CycloneDX SBOM: %w", err)
	}
	return nil
}

func encodeSBOM(inputs releaseInputs, inventory moduleInventory, report licenseReport) ([]byte, error) {
	bom, err := buildSBOM(inputs, inventory, report)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := cdx.NewBOMEncoder(&buffer, cdx.BOMFileFormatJSON).SetPretty(true).SetEscapeHTML(false)
	if err := encoder.EncodeVersion(bom, cdx.SpecVersion1_7); err != nil {
		return nil, fmt.Errorf("encode CycloneDX SBOM: %w", err)
	}
	return buffer.Bytes(), nil
}

func buildSBOM(inputs releaseInputs, inventory moduleInventory, report licenseReport) (*cdx.BOM, error) {
	licenses := make(map[string]componentLicenseEvidence, len(report.Components))
	for _, component := range report.Components {
		licenses[component.BOMRef] = component
	}
	var components []cdx.Component
	var rootComponent *cdx.Component
	for _, module := range inventory.Modules {
		component, err := moduleComponent(module, licenses[moduleBOMRef(module)])
		if err != nil {
			return nil, err
		}
		if module.Main {
			rootComponent = &component
		} else {
			components = append(components, component)
		}
	}
	if rootComponent == nil {
		return nil, errors.New("SBOM main component missing")
	}
	sort.Slice(components, func(i, j int) bool { return components[i].BOMRef < components[j].BOMRef })
	dependencies := moduleDependencies(inventory)
	bom := cdx.NewBOM()
	bom.SerialNumber = deterministicSerial(inputs, inventory.GraphDigest)
	bom.Metadata = sbomMetadata(inputs, inventory, rootComponent)
	bom.Components = &components
	bom.Dependencies = &dependencies
	return bom, nil
}

func sbomMetadata(inputs releaseInputs, inventory moduleInventory, rootComponent *cdx.Component) *cdx.Metadata {
	tools := []cdx.Component{{Type: cdx.ComponentTypeApplication, Name: "artemis-release", Version: inputs.Version, BOMRef: "tool:artemis-release@" + inputs.Version}}
	lifecycles := []cdx.Lifecycle{{Phase: cdx.LifecyclePhasePostBuild}}
	properties := []cdx.Property{
		{Name: "artemis:build-profile", Value: inputs.BuildProfile},
		{Name: "artemis:commit", Value: inputs.Commit},
		{Name: "artemis:module-graph-digest", Value: inventory.GraphDigest},
		{Name: "artemis:target", Value: inputs.Target.OS + "/" + inputs.Target.Arch},
		{Name: "artemis:toolchain-digest", Value: inputs.ToolchainDigest},
	}
	return &cdx.Metadata{Timestamp: inputs.generatedAt(), Lifecycles: &lifecycles, Tools: &cdx.ToolsChoice{Components: &tools}, Component: rootComponent, Properties: &properties}
}

func moduleComponent(module goModule, evidence componentLicenseEvidence) (cdx.Component, error) {
	if evidence.BOMRef != moduleBOMRef(module) || len(evidence.Licenses) == 0 {
		return cdx.Component{}, fmt.Errorf("module %s has no correlated license evidence", module.Path)
	}
	hash, err := moduleHash(module)
	if err != nil {
		return cdx.Component{}, fmt.Errorf("module %s hash: %w", module.Path, err)
	}
	licenses := make(cdx.Licenses, 0, len(evidence.Licenses))
	for _, item := range evidence.Licenses {
		license := cdx.License{Acknowledgement: cdx.LicenseAcknowledgementDeclared}
		if item.SPDXID != "" {
			license.ID = item.SPDXID
		} else {
			license.Name = item.Name
		}
		license.Properties = &[]cdx.Property{{Name: "artemis:license-path", Value: item.Path}, {Name: "artemis:license-sha256", Value: item.SHA256}}
		licenses = append(licenses, cdx.LicenseChoice{License: &license})
	}
	properties := []cdx.Property{{Name: "artemis:go-indirect", Value: fmt.Sprint(module.Indirect)}}
	if module.GoModSum != "" {
		properties = append(properties, cdx.Property{Name: "artemis:go-mod-sum", Value: module.GoModSum})
	}
	if module.Replace != nil {
		properties = append(properties,
			cdx.Property{Name: "artemis:replacement-path", Value: module.Replace.Path},
			cdx.Property{Name: "artemis:replacement-version", Value: module.Replace.Version},
			cdx.Property{Name: "artemis:replacement-digest", Value: module.Replace.Sum},
		)
	}
	hashes := []cdx.Hash{hash}
	modified := module.Replace != nil
	componentType := cdx.ComponentTypeLibrary
	if module.Main {
		componentType = cdx.ComponentTypeApplication
	}
	return cdx.Component{BOMRef: moduleBOMRef(module), Type: componentType, Name: module.Path, Version: module.Version, PackageURL: modulePURL(module.Path, module.Version), Hashes: &hashes, Licenses: &licenses, Properties: &properties, Modified: &modified}, nil
}

func moduleHash(module goModule) (cdx.Hash, error) {
	digest := module.Sum
	if module.Replace != nil {
		digest = module.Replace.Sum
	}
	if strings.HasPrefix(digest, "git:") {
		value := strings.TrimPrefix(digest, "git:")
		algorithm := cdx.HashAlgoSHA1
		if len(value) == 64 {
			algorithm = cdx.HashAlgoSHA256
		}
		return cdx.Hash{Algorithm: algorithm, Value: value}, nil
	}
	hexDigest, err := goChecksumHex(digest)
	if err != nil {
		return cdx.Hash{}, err
	}
	return cdx.Hash{Algorithm: cdx.HashAlgoSHA256, Value: strings.TrimPrefix(hexDigest, "sha256:")}, nil
}

func moduleDependencies(inventory moduleInventory) []cdx.Dependency {
	references := make(map[string]string, len(inventory.Modules))
	for _, module := range inventory.Modules {
		references[moduleGraphKey(module)] = moduleBOMRef(module)
	}
	dependencies := make([]cdx.Dependency, 0, len(inventory.Modules))
	for _, module := range inventory.Modules {
		var children []string
		for _, dependency := range inventory.Edges[moduleGraphKey(module)] {
			children = append(children, references[dependency])
		}
		children = sortedUnique(children)
		dependencies = append(dependencies, cdx.Dependency{Ref: moduleBOMRef(module), Dependencies: &children})
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Ref < dependencies[j].Ref })
	return dependencies
}

func deterministicSerial(inputs releaseInputs, moduleGraphDigest string) string {
	digest := stableDigestRaw(inputs.Version, inputs.Commit, inputs.BuildProfile, inputs.generatedAt(), inputs.Target.OS, inputs.Target.Arch, inputs.ToolchainDigest, moduleGraphDigest)
	decoded := append([]byte(nil), digest[:16]...)
	decoded[6] = decoded[6]&0x0f | 0x50
	decoded[8] = decoded[8]&0x3f | 0x80
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", decoded[0:4], decoded[4:6], decoded[6:8], decoded[8:10], decoded[10:16])
}

func validateSBOMFile(path string, inputs releaseInputs, inventory moduleInventory, report licenseReport) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	if err = validateCycloneDXSchema(data); err != nil {
		return err
	}
	expected, err := encodeSBOM(inputs, inventory, report)
	if err != nil {
		return fmt.Errorf("rebuild expected CycloneDX SBOM: %w", err)
	}
	if !bytes.Equal(data, expected) {
		return errors.New("CycloneDX document does not match canonical release inputs")
	}
	var bom cdx.BOM
	if err := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatJSON).Decode(&bom); err != nil {
		return fmt.Errorf("decode CycloneDX SBOM: %w", err)
	}
	if bom.BOMFormat != cdx.BOMFormat || bom.SpecVersion != cdx.SpecVersion1_7 || bom.SerialNumber != deterministicSerial(inputs, inventory.GraphDigest) {
		return errors.New("CycloneDX identity mismatch")
	}
	if bom.Metadata == nil || bom.Metadata.Timestamp != inputs.generatedAt() || bom.Metadata.Component == nil || bom.Metadata.Component.BOMRef == "" {
		return errors.New("CycloneDX metadata or root component missing")
	}
	if bom.Components == nil || len(*bom.Components) != len(inventory.Modules)-1 {
		return errors.New("CycloneDX component inventory is incomplete")
	}
	if bom.Dependencies == nil || len(*bom.Dependencies) != len(inventory.Modules) {
		return errors.New("CycloneDX dependency graph is incomplete")
	}
	if err := validateUniqueBOMRefs(bom); err != nil {
		return err
	}
	return validateSBOMComponents(bom, inventory, report)
}

func validateUniqueBOMRefs(bom cdx.BOM) error {
	if bom.Metadata == nil || bom.Metadata.Component == nil || bom.Components == nil {
		return errors.New("CycloneDX components are incomplete")
	}
	seen := make(map[string]struct{})
	components := []cdx.Component{*bom.Metadata.Component}
	components = append(components, (*bom.Components)...)
	if bom.Metadata.Tools != nil && bom.Metadata.Tools.Components != nil {
		components = append(components, (*bom.Metadata.Tools.Components)...)
	}
	for _, component := range components {
		if component.BOMRef == "" {
			return fmt.Errorf("CycloneDX component %s has no bom-ref", component.Name)
		}
		if _, duplicate := seen[component.BOMRef]; duplicate {
			return fmt.Errorf("duplicate CycloneDX bom-ref %s", component.BOMRef)
		}
		seen[component.BOMRef] = struct{}{}
	}
	return nil
}

func validateSBOMComponents(bom cdx.BOM, inventory moduleInventory, report licenseReport) error {
	components := make(map[string]cdx.Component, len(inventory.Modules))
	components[bom.Metadata.Component.BOMRef] = *bom.Metadata.Component
	for _, component := range *bom.Components {
		if component.BOMRef == "" {
			return errors.New("CycloneDX component has no bom-ref")
		}
		if _, duplicate := components[component.BOMRef]; duplicate {
			return fmt.Errorf("duplicate CycloneDX component %s", component.BOMRef)
		}
		components[component.BOMRef] = component
	}
	for _, evidence := range report.Components {
		component, ok := components[evidence.BOMRef]
		if !ok || component.Hashes == nil || len(*component.Hashes) == 0 || component.Licenses == nil || len(*component.Licenses) != len(evidence.Licenses) {
			return fmt.Errorf("CycloneDX component %s lacks hash or license correlation", evidence.BOMRef)
		}
	}
	expected := moduleDependencies(inventory)
	actual := append([]cdx.Dependency(nil), (*bom.Dependencies)...)
	sort.Slice(actual, func(i, j int) bool { return actual[i].Ref < actual[j].Ref })
	if !equalDependencies(actual, expected) {
		return errors.New("CycloneDX dependency edges do not match the Go module graph")
	}
	return nil
}

func equalDependencies(actual, expected []cdx.Dependency) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index].Ref != expected[index].Ref {
			return false
		}
		actualChildren := []string(nil)
		expectedChildren := []string(nil)
		if actual[index].Dependencies != nil {
			actualChildren = *actual[index].Dependencies
		}
		if expected[index].Dependencies != nil {
			expectedChildren = *expected[index].Dependencies
		}
		if len(actualChildren) != len(expectedChildren) {
			return false
		}
		for childIndex := range actualChildren {
			if actualChildren[childIndex] != expectedChildren[childIndex] {
				return false
			}
		}
	}
	return true
}
