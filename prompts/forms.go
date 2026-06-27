package prompts

// forms.go (spec L4029: prompts/forms.go - form filling system
// prompt).
//
// LLM prompt templates: form filling system prompt that provides
// rules for form interaction. This file is the spec-mandated facade
// for the form filling prompt defined in prompts.go.
//
// Ref: research/agents/agentscope-main/examples/agent/browser_agent/
// build_in_prompt/browser_agent_form_filling_sys_prompt.md:1-16

// FormFillingPrompt returns the form filling system prompt
// (spec L4029: forms.go - form filling).
func FormFillingPrompt() string {
	return formFillingPrompt
}

// FormFillingPromptType returns the PromptType for form filling
// (spec L4029: forms.go).
func FormFillingPromptType() PromptType {
	return PromptFormFilling
}

// FormFillingPromptTemplate returns the form filling prompt as a
// PromptTemplate (spec L4029: forms.go - form filling).
func FormFillingPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "form_filling",
		Type:     PromptFormFilling,
		Template: formFillingPrompt,
	}
}
