package fixture

// CrossResult is the observable evidence a runner can return for a fixture.
type CrossResult struct {
	URL        string
	StatusCode int
	Title      string
	Text       string
	HTML       string
	Markdown   string
	EvalResult string
	EvalErr    string
	Links      []string
}
