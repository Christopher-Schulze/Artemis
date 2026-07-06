package prompts

// decompose.go (spec L4029: prompts/decompose.go - task decomposition
// prompt).
//
// LLM prompt templates: task decomposition prompt that breaks complex
// tasks into atomic subtasks. This file is the spec-mandated facade
// for the decomposition prompts defined in prompts.go.
//
// Ref: research/agents/agentscope-main/examples/agent/browser_agent/
// build_in_prompt/browser_agent_task_decomposition_prompt.md:1-28

// DecomposePrompt returns the task decomposition prompt
// (spec L4029: decompose.go - task decomposition).
func DecomposePrompt() string {
	return taskDecompositionPrompt
}

// DecomposePromptType returns the PromptType for task decomposition
// (spec L4029: decompose.go).
func DecomposePromptType() PromptType {
	return PromptTaskDecomposition
}

// DecomposePromptTemplate returns the decomposition prompt as a
// PromptTemplate (spec L4029: decompose.go - task decomposition).
func DecomposePromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "task_decomposition",
		Type:     PromptTaskDecomposition,
		Template: taskDecompositionPrompt,
	}
}

// DecomposeReflectionPrompt returns the decomposition reflection prompt
// (spec L4029: decompose.go - task decomposition).
func DecomposeReflectionPrompt() string {
	return decomposeReflectionPrompt
}

// DecomposeReflectionPromptType returns the PromptType for decomposition
// reflection (spec L4029: decompose.go).
func DecomposeReflectionPromptType() PromptType {
	return PromptDecomposeReflection
}

// DecomposeReflectionPromptTemplate returns the reflection prompt as a
// PromptTemplate (spec L4029: decompose.go).
func DecomposeReflectionPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "decompose_reflection",
		Type:     PromptDecomposeReflection,
		Template: decomposeReflectionPrompt,
	}
}
