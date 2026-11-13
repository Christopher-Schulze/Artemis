package prompts

// observe.go (prompts/observe.go - chunked observation
// reasoning prompt).
//
// LLM prompt templates: chunked observation reasoning prompt for
// pages >80KB. This file is the spec-mandated facade for the
// observation prompts defined in prompts.go.
//
// Ref: github.com/agentscope-ai/agentscope/examples/agent/browser_agent/
// build_in_prompt/browser_agent_observe_reasoning_prompt.md:1-19

// ObservePrompt returns the observation reasoning prompt
// (observe.go - chunked observation).
func ObservePrompt() string {
	return observeReasoningPrompt
}

// ObservePromptType returns the PromptType for observation reasoning
// (observe.go).
func ObservePromptType() PromptType {
	return PromptObserveReasoning
}

// ObservePromptTemplate returns the observation prompt as a
// PromptTemplate (observe.go - chunked observation).
func ObservePromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "observe_reasoning",
		Type:     PromptObserveReasoning,
		Template: observeReasoningPrompt,
	}
}

// PureReasoningPrompt returns the pure reasoning prompt
// (observe.go - chunked observation).
func PureReasoningPrompt() string {
	return pureReasoningPrompt
}

// PureReasoningPromptType returns the PromptType for pure reasoning
// (observe.go).
func PureReasoningPromptType() PromptType {
	return PromptPureReasoning
}

// PureReasoningPromptTemplate returns the pure reasoning prompt as a
// PromptTemplate (observe.go).
func PureReasoningPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "pure_reasoning",
		Type:     PromptPureReasoning,
		Template: pureReasoningPrompt,
	}
}
