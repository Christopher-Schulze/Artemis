package prompts

// forms.go (prompts/forms.go - form filling system
// prompt).
//
// LLM prompt templates: form filling system prompt that provides
// rules for form interaction. This file is the spec-mandated facade
// for the form filling prompt defined in prompts.go.
//
// Ref: github.com/agentscope-ai/agentscope/examples/agent/browser_agent/
// build_in_prompt/browser_agent_form_filling_sys_prompt.md:1-16

// FormFillingPrompt returns the form filling system prompt
// (forms.go - form filling).
func FormFillingPrompt() string {
	return formFillingPrompt
}

// FormFillingPromptType returns the PromptType for form filling
// (forms.go).
func FormFillingPromptType() PromptType {
	return PromptFormFilling
}

// FormFillingPromptTemplate returns the form filling prompt as a
// PromptTemplate (forms.go - form filling).
func FormFillingPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "form_filling",
		Type:     PromptFormFilling,
		Template: formFillingPrompt,
	}
}
