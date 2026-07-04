# TASK 006b (Phase 5b): Forms + Actions

## Why

After extraction the agent's next move is interaction: click a button, fill a form, submit. TASK 006b lands the form interaction surface and the higher-level click/type helpers, plus the engine plumbing (`FetchOpts.Method`, `FetchOpts.Body`, `Page.Submit`) needed to follow a form submission to the next page.

## Acceptance

- `agent/forms.go`:
  - `Form` struct: `Action`, `Method`, `EncType`, `Fields []FormField`, internal node ref.
  - `FormField`: `Name`, `Type`, `Value`, `Checked`, `Options []string` (for selects).
  - `Forms(doc) []*Form` returns every form on the page.
  - `FindForm(doc, selector) *Form` returns the first form matching a CSS selector (or nil).
  - `(*Form).Set(name, value)` updates the corresponding input/textarea/select node in the Go DOM.
  - `(*Form).Toggle(name, checked bool)` for checkboxes/radios.
  - `(*Form).Submit() (FormSubmission, error)` returns the resolved URL, method, content-type, body for POSTing the form.
  - URL-encoded encoding only; multipart files arrive in a later TASK.
- `agent/actions.go`:
  - `ClickByText(doc, text) (*webapi.Node, bool)` finds the first button/anchor whose normalized text matches and returns the element. Caller dispatches the click in JS via `page.Eval(... .click())` or via `page.Click(node)`.
  - `Type(doc, selector, text)` sets the value attribute on the input matching selector.
- `engine/page.go`:
  - `FetchOpts` gains `Method string`, `Body []byte`, `ContentType string`.
  - `Page.Submit(ctx, sub agent.FormSubmission, opts FetchOpts) (*Page, error)` performs the submission and returns the next page.
  - `Page.Click(node *webapi.Node) error` runs `node.click()` via JS.
- `make build`, `make vet`, `make test`, `go test -race ./js ./engine ./agent` all green.

- [x] mark active, write detail
- [x] `agent/forms.go` (Form, FormField, FormSubmission, Forms, FindForm, Set, Toggle, Submit GET+POST URL-encoded) + tests
- [x] `agent/actions.go` (ClickByText for button/a/input[submit], Type for input/textarea) + tests
- [x] `engine.FetchOpts` gains Method, Body, ContentType, Navigator
- [x] `engine.Engine.Submit` performs the form submission
- [x] `engine.Page.Click(ctx, node)` dispatches click via `__wrap(handle).click()` in JS
- [x] `js.Context.HandleFor(node)` exposes handle table
- [x] integration test: form page -> Set -> Submit -> backend receives POST with correct CT and body, next page rendered
- [x] integration test: button + JS listener -> ClickByText + page.Click -> listener fires, DOM mutates, eval confirms
- [x] all green: build, vet, test, race
- [x] FLUSH documentation.md
- [x] archive

## Notes

`Form.Submit` resolves the form's `action` against the document URL. Method is normalized to upper-case; default is `GET`. Default enctype is `application/x-www-form-urlencoded`. For GET forms the values are appended to the URL as a query string and the body is empty.

`Type(doc, selector, text)` does NOT dispatch input/change events in this TASK. Many agent flows are happy with just the value mutation; React-style controlled inputs that listen for input events need event dispatch which lands when MutationObserver / synthetic events arrive in TASK 004e.

Reference: internal design notes.

## Deviations

(none yet)
