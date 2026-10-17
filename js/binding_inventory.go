package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/parser"
	v8 "github.com/tommie/v8go"
)

// InstalledBinding records a descriptor, without invoking its getter. AliasOf
// identifies shared objects; [[Prototype]] edges retain inherited properties.
// Presence is not a semantic support or execution-authority claim.
type InstalledBinding struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	AliasOf   string `json:"alias_of,omitempty"`
	HasGetter bool   `json:"has_getter,omitempty"`
	HasSetter bool   `json:"has_setter,omitempty"`
}

// BindingInventory is a source-pinned observation of a fresh, owned realm.
// It includes native/internal descriptors, not page-supplied state.
type BindingInventory struct {
	SourceHash    string             `json:"source_hash"`
	Bindings      []InstalledBinding `json:"bindings"`
	NativeGlobals []string           `json:"native_globals"`
}

// ObserveInstalledBindings constructs and joins its own ordinary Context. It
// never evaluates page code, starts a server or writes generated artifacts.
func ObserveInstalledBindings(ctx context.Context) (BindingInventory, error) {
	if ctx == nil {
		return BindingInventory{}, errors.New("binding inventory: context required")
	}
	if err := ctx.Err(); err != nil {
		return BindingInventory{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	doc, err := parser.ParseHTML(strings.NewReader("<html><body></body></html>"), "https://inventory.invalid/")
	if err != nil {
		return BindingInventory{}, err
	}
	runtime := NewRuntime()
	defer runtime.Close()
	realm, err := runtime.NewContext(doc, ContextOpts{RequestContext: ctx})
	if err != nil {
		return BindingInventory{}, err
	}
	defer realm.Close()
	encoded, err := realm.EvalString(ctx, installedBindingObservation)
	if err != nil {
		return BindingInventory{}, fmt.Errorf("binding inventory: %w", err)
	}
	result, err := decodeBindingInventory(encoded)
	if err != nil {
		return BindingInventory{}, err
	}
	result.NativeGlobals, err = observeNativeGlobalNames()
	if err == nil {
		err = ctx.Err()
	}
	return result, err
}

func observeNativeGlobalNames() ([]string, error) {
	isolate := v8.NewIsolate()
	defer isolate.Dispose()
	realm := v8.NewContext(isolate)
	defer realm.Close()
	value, err := realm.RunScript("JSON.stringify(Object.getOwnPropertyNames(globalThis).sort())", "<native-binding-baseline>")
	if err != nil {
		return nil, err
	}
	defer value.Release()
	encoded, err := value.StringChecked()
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(encoded), &names); err != nil {
		return nil, err
	}
	return names, nil
}

func decodeBindingInventory(encoded string) (BindingInventory, error) {
	if len(encoded) > 4<<20 {
		return BindingInventory{}, errors.New("binding inventory: byte bound exceeded")
	}
	var result BindingInventory
	if err := json.Unmarshal([]byte(encoded), &result.Bindings); err != nil {
		return BindingInventory{}, fmt.Errorf("binding inventory: %w", err)
	}
	if len(result.Bindings) == 0 || len(result.Bindings) > 16384 {
		return BindingInventory{}, errors.New("binding inventory: descriptor bound exceeded")
	}
	slices.SortFunc(result.Bindings, func(a, b InstalledBinding) int { return strings.Compare(a.Path, b.Path) })
	if err := validateInstalledBindings(result.Bindings); err != nil {
		return BindingInventory{}, err
	}
	var err error
	result.SourceHash, err = SourceHash()
	return result, err
}

func validateInstalledBindings(bindings []InstalledBinding) error {
	seen := make(map[string]InstalledBinding, len(bindings))
	for _, binding := range bindings {
		if _, duplicate := seen[binding.Path]; duplicate || binding.Path == "" || len(binding.Path) > 8192 {
			return fmt.Errorf("binding inventory: duplicate/invalid path %q", binding.Path)
		}
		switch binding.Kind {
		case "object", "function", "accessor", "undefined", "null", "string", "symbol", "number", "bigint", "boolean":
		default:
			return fmt.Errorf("binding inventory: invalid descriptor kind %q", binding.Kind)
		}
		seen[binding.Path] = binding
	}
	if root, exists := seen["$"]; !exists || root.Kind != "object" || root.AliasOf != "" {
		return errors.New("binding inventory: actual global root required")
	}
	for _, binding := range bindings {
		if binding.AliasOf == "" {
			continue
		}
		owner, exists := seen[binding.AliasOf]
		if !exists || owner.AliasOf != "" || owner.Path == binding.Path || owner.Kind != binding.Kind || (binding.Kind != "object" && binding.Kind != "function") {
			return fmt.Errorf("binding inventory: invalid alias owner %q", binding.AliasOf)
		}
	}
	return nil
}

const installedBindingObservation = `(()=>{
  const entries=[], seen=new WeakMap();
  const propertyPath=(owner,key)=>{
    const name=typeof key==='symbol'?'['+String(key)+']':key;
    return owner==='$'?name:owner+'.'+name;
  };
  function visit(path,value){
    if(entries.length>=16384)throw Error('binding inventory descriptor limit');
    const kind=value===null?'null':typeof value;
    const entry={path,kind}; entries.push(entry);
    if(value===null||(kind!=='object'&&kind!=='function'))return;
    const owner=seen.get(value);
    if(owner!==undefined){entry.alias_of=owner;return;}
    seen.set(value,path);
    const keys=Reflect.ownKeys(value).sort((a,b)=>String(a)<String(b)?-1:String(a)>String(b)?1:0);
    for(const key of keys){
      const descriptor=Object.getOwnPropertyDescriptor(value,key), child=propertyPath(path,key);
      if('value' in descriptor)visit(child,descriptor.value);
      else {
        if(entries.length>=16384)throw Error('binding inventory descriptor limit');
        entries.push({path:child,kind:'accessor',has_getter:typeof descriptor.get==='function',has_setter:typeof descriptor.set==='function'});
      }
    }
    const prototype=Object.getPrototypeOf(value);
    if(prototype!==null)visit(path+'.[[Prototype]]',prototype);
  }
  visit('$',globalThis);
  for(const [name,tag] of [['@element','div'],['@input','input'],['@form','form'],['@iframe','iframe'],['@canvas','canvas']])visit(name,document.createElement(tag));
  return JSON.stringify(entries);
})()`
