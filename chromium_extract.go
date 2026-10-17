package artemis

import (
	"context"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/agent"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/webapi"
	"golang.org/x/net/html"
)

// DumpChromiumPage reads the current target through the existing extraction owner.
func (s *Session) DumpChromiumPage(ctx context.Context, pageID, format string) (any, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return nil, taskErr
	}
	defer done()
	page, release, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return nil, taskErr
	}
	defer release.Release()
	execCtx = release.Context()
	if taskErr := s.authorizeChromiumOperation(execCtx, page, "extract", ""); taskErr != nil {
		return nil, taskErr
	}
	output, err := (artemisrouter.ChromiumExecutor{Page: page.page, Observer: page.observer}).Read(execCtx, artemisrouter.BrowserState{SessionID: s.id})
	if err != nil {
		return nil, classifyTaskError("chromium_dump", err)
	}
	switch format {
	case "html":
		return output.Page.HTML, nil
	case "markdown", "md", "":
		return output.Page.Markdown, nil
	case "text":
		return output.Page.Text, nil
	case "title":
		return output.Page.Title, nil
	case "links":
		return output.Page.Links, nil
	case "structured", "semantic":
		root, err := html.Parse(strings.NewReader(output.Page.HTML))
		if err != nil {
			return nil, classifyTaskError("chromium_dump", err)
		}
		doc := webapi.NewDocument(root, output.Page.URL)
		if format == "structured" {
			return agent.Structured(doc), nil
		}
		return agent.SemanticString(agent.Semantic(doc)), nil
	default:
		return nil, newTaskError(TaskErrorInvalidInput, "chromium_dump", fmt.Errorf("unknown format %q", format))
	}
}
