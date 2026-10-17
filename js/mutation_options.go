package js

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	v8 "github.com/tommie/v8go"
)

const (
	maxMutationFilterNames = 1024
	maxMutationFilterBytes = 64 << 10
)

type observerOptions struct {
	childList, attributes, characterData, subtree bool
	attributeOldValue, characterDataOldValue      bool
	attributeFilter                               []string
}

func (r *Runtime) registerMutationObserver(info *v8.FunctionCallbackInfo) *v8.Value {
	c, args := r.contextFor(info.Context()), info.Args()
	retained := false
	defer func() {
		info.This().Release()
		for index, arg := range args {
			if index != 2 || !retained {
				arg.Release()
			}
		}
	}()
	if c == nil || len(args) < 3 {
		return r.responseError(info, errors.New("mutation observer: target, options and callback required"))
	}
	target, err := mutationInteger(args[0], math.MaxUint32)
	if err != nil {
		return r.responseError(info, err)
	}
	if c.nodes.Get(uint32(target)) == nil {
		return r.responseError(info, errors.New("mutation observer: valid target required"))
	}
	object, err := args[1].AsObject()
	if err != nil {
		return r.responseError(info, err)
	}
	options, err := readMutationOptions(object)
	if err != nil {
		return r.responseError(info, err)
	}
	callback, err := args[2].AsFunction()
	if err != nil {
		return r.responseError(info, err)
	}
	var existing, order uint64
	if len(args) > 3 && !args[3].IsNullOrUndefined() {
		existing, err = mutationInteger(args[3], math.MaxInt32)
		if err != nil {
			return r.responseError(info, err)
		}
	}
	if len(args) > 4 {
		order, err = mutationInteger(args[4], 9007199254740991)
		if err != nil {
			return r.responseError(info, err)
		}
	}
	owner, created, err := c.observers.register(uint32(target), uint32(existing), order, options, callback)
	if err != nil {
		return r.responseError(info, err)
	}
	retained = created
	return r.callbackPrimitive(info, owner.id)
}

func mutationInteger(value *v8.Value, maximum float64) (uint64, error) {
	if !value.IsNumber() {
		return 0, errors.New("mutation observer: integer identity required")
	}
	number := value.Number()
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > maximum || math.Trunc(number) != number {
		return 0, errors.New("mutation observer: integer identity out of range")
	}
	return uint64(number), nil
}

func readMutationOptions(object *v8.Object) (observerOptions, error) {
	values := make(map[string]*v8.Value, 7)
	defer func() {
		for _, value := range values {
			value.Release()
		}
	}()
	for _, name := range []string{"attributeFilter", "attributeOldValue", "attributes", "characterData", "characterDataOldValue", "childList", "subtree"} {
		value, err := object.Get(name)
		if err != nil {
			return observerOptions{}, err
		}
		values[name] = value
	}
	options := observerOptions{childList: values["childList"].Boolean(), attributes: values["attributes"].Boolean(),
		characterData: values["characterData"].Boolean(), subtree: values["subtree"].Boolean(),
		attributeOldValue: values["attributeOldValue"].Boolean(), characterDataOldValue: values["characterDataOldValue"].Boolean()}
	if values["attributes"].IsUndefined() && (!values["attributeOldValue"].IsUndefined() || !values["attributeFilter"].IsUndefined()) {
		options.attributes = true
	}
	if values["characterData"].IsUndefined() && !values["characterDataOldValue"].IsUndefined() {
		options.characterData = true
	}
	if !options.childList && !options.attributes && !options.characterData {
		return observerOptions{}, errors.New("mutation observer: observation type required")
	}
	if !options.attributes && (options.attributeOldValue || !values["attributeFilter"].IsUndefined()) || !options.characterData && options.characterDataOldValue {
		return observerOptions{}, errors.New("mutation observer: old value/filter conflicts with disabled observation")
	}
	if !values["attributeFilter"].IsUndefined() {
		filter, err := readMutationFilter(values["attributeFilter"])
		if err != nil {
			return observerOptions{}, err
		}
		options.attributeFilter = filter
	}
	return options, nil
}

func readMutationFilter(value *v8.Value) ([]string, error) {
	if !value.IsArray() {
		return nil, errors.New("mutation observer: normalized attribute filter array required")
	}
	object, err := value.AsObject()
	if err != nil {
		return nil, err
	}
	length, err := object.Get("length")
	if err != nil {
		return nil, err
	}
	defer length.Release()
	if !length.IsNumber() {
		return nil, errors.New("mutation observer: attribute filter length must be numeric")
	}
	number := length.Number()
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > maxMutationFilterNames || math.Trunc(number) != number {
		return nil, errors.New("mutation observer: attribute filter name limit exceeded")
	}
	filter := make([]string, int(number))
	bytes := 0
	for index := range filter {
		entry, err := object.GetIdx(uint32(index))
		if err != nil {
			return nil, err
		}
		if !entry.IsString() {
			entry.Release()
			return nil, errors.New("mutation observer: normalized attribute filter string required")
		}
		text, conversionErr := entry.StringWithLimit(maxMutationFilterBytes - bytes)
		entry.Release()
		if conversionErr != nil {
			return nil, fmt.Errorf("mutation observer: attribute filter byte limit exceeded: %w", conversionErr)
		}
		bytes += len(text)
		filter[index] = text
	}
	return filter, nil
}

func (options observerOptions) retainedBytes() int {
	bytes := int(unsafe.Sizeof(options)) + cap(options.attributeFilter)*int(unsafe.Sizeof(""))
	for _, name := range options.attributeFilter {
		bytes += len(name)
	}
	return bytes
}

func (options observerOptions) matches(record mutationRecord) bool {
	switch record.Type {
	case MutationChildList:
		return options.childList
	case MutationCharacterData:
		return options.characterData
	case MutationAttributes:
		if !options.attributes {
			return false
		}
		if options.attributeFilter == nil {
			return true
		}
		for _, name := range options.attributeFilter {
			if name == record.AttributeName {
				return true
			}
		}
	}
	return false
}

func (options observerOptions) retainsOldValue(kind MutationRecordType) bool {
	return kind == MutationAttributes && options.attributeOldValue || kind == MutationCharacterData && options.characterDataOldValue
}
