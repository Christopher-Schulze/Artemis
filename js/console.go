package js

import (
	"strings"
	"sync"

	v8 "rogchap.com/v8go"
)

// Console is the sink for console.* calls.
type Console interface {
	Log(level, msg string)
}

// FuncConsole adapts a function to the Console interface.
type FuncConsole func(level, msg string)

// Log forwards level and msg to the underlying function.
func (f FuncConsole) Log(level, msg string) {
	if f != nil {
		f(level, msg)
	}
}

// DiscardConsole drops all output.
type DiscardConsole struct{}

// Log on DiscardConsole is a no-op.
func (DiscardConsole) Log(string, string) {}

// ConsoleEntry is a captured console call.
type ConsoleEntry struct {
	Level string
	Msg   string
}

// CollectConsole accumulates entries in memory; intended as a test helper
// and as a building block for higher-level capture.
type CollectConsole struct {
	mu      sync.Mutex
	Entries []ConsoleEntry
}

// Log appends an entry.
func (c *CollectConsole) Log(level, msg string) {
	c.mu.Lock()
	c.Entries = append(c.Entries, ConsoleEntry{Level: level, Msg: msg})
	c.mu.Unlock()
}

// Snapshot returns a copy of the entries observed so far.
func (c *CollectConsole) Snapshot() []ConsoleEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ConsoleEntry, len(c.Entries))
	copy(out, c.Entries)
	return out
}

// consoleTemplates caches the 5 console method templates at Runtime
// level. Each template's callback resolves *Context via the Runtime
// registry so the templates can be shared across all Contexts derived
// from the same Isolate.
type consoleTemplates struct {
	objTmpl *v8.ObjectTemplate
	log     *v8.FunctionTemplate
	warn    *v8.FunctionTemplate
	errorFn *v8.FunctionTemplate
	info    *v8.FunctionTemplate
	debug   *v8.FunctionTemplate
}

func (r *Runtime) ensureConsoleTemplates() *consoleTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.consoleTemplates != nil {
		return r.consoleTemplates
	}
	iso := r.iso
	mk := func(level string) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			if c == nil || c.console == nil {
				return nil
			}
			args := info.Args()
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = a.String()
			}
			c.console.Log(level, strings.Join(parts, " "))
			return nil
		})
	}
	r.consoleTemplates = &consoleTemplates{
		objTmpl: v8.NewObjectTemplate(iso),
		log:     mk("log"),
		warn:    mk("warn"),
		errorFn: mk("error"),
		info:    mk("info"),
		debug:   mk("debug"),
	}
	return r.consoleTemplates
}

func installConsole(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	ct := c.rt.ensureConsoleTemplates()
	obj, err := ct.objTmpl.NewInstance(v8ctx)
	if err != nil {
		return err
	}
	if err := obj.Set("log", ct.log.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := obj.Set("warn", ct.warn.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := obj.Set("error", ct.errorFn.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := obj.Set("info", ct.info.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := obj.Set("debug", ct.debug.GetFunction(v8ctx)); err != nil {
		return err
	}
	return v8ctx.Global().Set("console", obj)
}
