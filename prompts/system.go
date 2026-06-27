package prompts

// system.go (spec L4029: prompts/system.go - base behavior system
// prompt).
//
// LLM prompt templates: base behavior system prompt for the browser
// agent. This file is the spec-mandated facade for the system prompt
// defined in prompts.go.
//
// Ref: research/agents/agentscope-main/examples/agent/browser_agent/
// build_in_prompt/browser_agent_sys_prompt.md:1-57

// SystemPrompt returns the base behavior system prompt
// (spec L4029: system.go - base behavior).
func SystemPrompt() string {
	return systemPrompt
}

// SystemPromptType returns the PromptType for the system prompt
// (spec L4029: system.go).
func SystemPromptType() PromptType {
	return PromptSystem
}

// SystemPromptTemplate returns the system prompt as a PromptTemplate
// (spec L4029: system.go - base behavior).
func SystemPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "system",
		Type:     PromptSystem,
		Template: systemPrompt,
	}
}
