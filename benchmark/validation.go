package benchmark

import (
	"crypto/sha256"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// ValidationEvidence records the identical extraction assertions for both engines.
type ValidationEvidence struct {
	Title          string `json:"title"`
	Links          int    `json:"links"`
	Paragraphs     int    `json:"paragraphs"`
	TextSHA256     string `json:"textSHA256"`
	MarkdownSHA256 string `json:"markdownSHA256"`
	Passed         bool   `json:"passed"`
}

func validateDocument(scenario Scenario, document *webapi.Document) ValidationEvidence {
	if document == nil {
		return ValidationEvidence{}
	}
	text := agent.Text(document)
	markdown := agent.Markdown(document)
	links := agent.Links(document)
	evidence := ValidationEvidence{Title: document.Title(), Links: len(links),
		TextSHA256:     fmt.Sprintf("%x", sha256.Sum256([]byte(text))),
		MarkdownSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(markdown)))}
	evidence.Paragraphs = len(webapi.GetElementsByTagName(document.Root(), "p"))
	evidence.Passed = evidence.Title == scenario.ExpectTitle && evidence.Links >= scenario.ExpectLinks && evidence.Paragraphs >= scenario.ExpectParagraphs && text != "" && markdown != ""
	return evidence
}
