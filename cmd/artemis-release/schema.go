package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	cyclonedxSchemaID = "http://cyclonedx.org/schema/bom-1.7.schema.json"
	spdxSchemaID      = "http://cyclonedx.org/schema/spdx.schema.json"
	jsfSchemaID       = "http://cyclonedx.org/schema/jsf-0.82.schema.json"
	cryptoSchemaID    = "http://cyclonedx.org/schema/cryptography-defs.schema.json"
)

//go:embed schema/bom-1.7.schema.json
var cyclonedx17Schema string

//go:embed schema/spdx.schema.json
var cyclonedxSPDXSchema string

//go:embed schema/jsf-0.82.schema.json
var cyclonedxJSFSchema string

//go:embed schema/cryptography-defs.schema.json
var cyclonedxCryptoSchema string

var cyclonedxSchemaCache struct {
	sync.Once
	schema *jsonschema.Schema
	err    error
}

func validateCycloneDXSchema(document []byte) error {
	cyclonedxSchemaCache.Do(compileCycloneDXSchema)
	if cyclonedxSchemaCache.err != nil {
		return cyclonedxSchemaCache.err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return fmt.Errorf("decode CycloneDX JSON document: %w", err)
	}
	if err := cyclonedxSchemaCache.schema.Validate(instance); err != nil {
		return fmt.Errorf("CycloneDX 1.7 JSON schema rejected document: %w", err)
	}
	return nil
}

func compileCycloneDXSchema() {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)
	resources := []struct {
		name string
		id   string
		data string
	}{
		{name: "SPDX", id: spdxSchemaID, data: cyclonedxSPDXSchema},
		{name: "JSF", id: jsfSchemaID, data: cyclonedxJSFSchema},
		{name: "cryptography", id: cryptoSchemaID, data: cyclonedxCryptoSchema},
		{name: "1.7", id: cyclonedxSchemaID, data: cyclonedx17Schema},
	}
	for _, resource := range resources {
		if err := addSchemaResource(compiler, resource.id, resource.data); err != nil {
			cyclonedxSchemaCache.err = fmt.Errorf("load CycloneDX %s schema: %w", resource.name, err)
			return
		}
	}
	cyclonedxSchemaCache.schema, cyclonedxSchemaCache.err = compiler.Compile(cyclonedxSchemaID)
	if cyclonedxSchemaCache.err != nil {
		cyclonedxSchemaCache.err = fmt.Errorf("compile CycloneDX 1.7 JSON schema: %w", cyclonedxSchemaCache.err)
	}
}

func addSchemaResource(compiler *jsonschema.Compiler, id, data string) error {
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := compiler.AddResource(id, document); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	return nil
}
