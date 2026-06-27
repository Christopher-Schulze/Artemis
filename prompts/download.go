package prompts

// download.go (spec L4029: prompts/download.go - file download
// system prompt).
//
// LLM prompt templates: file download system prompt that provides
// rules for file download. This file is the spec-mandated facade for
// the file download prompt defined in prompts.go.
//
// Ref: research/agents/agentscope-main/examples/agent/browser_agent/
// build_in_prompt/browser_agent_file_download_sys_prompt.md:1-8

// FileDownloadPrompt returns the file download system prompt
// (spec L4029: download.go - file download).
func FileDownloadPrompt() string {
	return fileDownloadPrompt
}

// FileDownloadPromptType returns the PromptType for file download
// (spec L4029: download.go).
func FileDownloadPromptType() PromptType {
	return PromptFileDownload
}

// FileDownloadPromptTemplate returns the file download prompt as a
// PromptTemplate (spec L4029: download.go - file download).
func FileDownloadPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Name:     "file_download",
		Type:     PromptFileDownload,
		Template: fileDownloadPrompt,
	}
}
