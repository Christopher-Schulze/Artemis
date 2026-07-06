package prompts

// summarize.go (spec L4029: prompts/summarize.go - task
// summarization prompt).
//
// LLM prompt templates: task summarization prompt that generates the
// final structured report. This file is the spec-mandated facade for
// the summarize prompt defined in prompts.go.
//
// Ref: research/agents/agentscope-main/examples/agent/browser_agent/
// build_in_prompt/browser_agent_summarize_task.md:1-20

// SummarizePrompt returns the task summarization prompt
// (spec L4029: summarize.go - task summarization).
func SummarizePrompt() string {
	return summarizeTaskPrompt
}

// SummarizePromptType returns the PromptType for task summarization
// (spec L4029: summarize.go).
func SummarizePromptType() PromptType {
	return PromptSummarizeTask
}

// SummarizePromptTemplate returns the summarize prompt as a
// PromptTemplate (spec L4029: summarize.go - task summarization).
func SummarizePromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "summarize_task",
		Type:     PromptSummarizeTask,
		Template: summarizeTaskPrompt,
	}
}
