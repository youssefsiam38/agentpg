package compaction

// SummarizationSystemPrompt is the system prompt used for context summarization.
// This prompt instructs Claude to create a structured summary that preserves
// critical information from the conversation being compacted.
//
// The 9-section structure is based on production patterns from AI coding assistants
// like Claude Code, Aider, and Cline.
const SummarizationSystemPrompt = `You are a conversation summarizer for an AI agent system. Your task is to create a comprehensive summary of the conversation that will replace the original messages while preserving all critical context.

Create a structured summary with the following 9 sections. Each section should capture the relevant information from the conversation. If a section has no relevant content, write "None" for that section.

## Format

1. **Primary Request and Intent**
   - The user's main goal or request
   - Any constraints or requirements specified
   - The overall context of what they're trying to accomplish

2. **Key Technical Concepts**
   - Important technical terms, APIs, or frameworks discussed
   - Design patterns or architectural decisions made
   - Any domain-specific knowledge established

3. **Files and Code Sections**
   - Files that were created, modified, or discussed
   - Key code snippets or implementations
   - Important file paths and their purposes

4. **Errors and Fixes**
   - Errors encountered during the conversation
   - Solutions that were applied
   - Workarounds or alternatives discussed

5. **Problem Solving**
   - The approach taken to solve problems
   - Alternatives that were considered
   - Reasoning behind decisions made

6. **User Preferences and Constraints**
   - Any preferences the user expressed
   - Constraints or limitations mentioned
   - Style or formatting preferences

7. **Pending Tasks**
   - Tasks mentioned but not yet started
   - Follow-up items discussed
   - Future work planned

8. **Current Work**
   - What was being actively worked on
   - The current state of any implementations
   - Progress made so far

9. **Next Step**
   - The immediate next action to take
   - What the agent should do when resuming
   - Any context needed for continuation

## Guidelines

- Be concise but complete - preserve all information needed to continue the conversation
- Use bullet points for clarity
- Include specific details (file names, function names, error messages)
- Maintain the chronological order of events within each section
- Preserve exact user quotes when they convey important intent
- Do not add information that wasn't in the original conversation
- Focus on actionable information over commentary`

// BuildSummarizationUserPrompt creates the user message for summarization.
// It includes the messages to be summarized and any additional context.
func BuildSummarizationUserPrompt(conversationText string) string {
	return `Please summarize the following conversation according to the format specified in your instructions.

<conversation>
` + conversationText + `
</conversation>

Create a comprehensive summary that will allow continuation of this conversation with full context. Follow the 9-section format exactly.`
}

// BuildSummarizationUserPromptWithContext creates the user message for summarization
// with additional context from preserved messages.
func BuildSummarizationUserPromptWithContext(contextText, conversationText string) string {
	prompt := `Please summarize the following conversation according to the format specified in your instructions.

`
	if contextText != "" {
		prompt += `<previous_context>
` + contextText + `
</previous_context>

The above is preserved context from earlier in the conversation. Incorporate relevant information from this context into your summary.

`
	}

	prompt += `<conversation_to_summarize>
` + conversationText + `
</conversation_to_summarize>

Create a comprehensive summary that will allow continuation of this conversation with full context. Follow the 9-section format exactly.`

	return prompt
}

// FormatMessagesAsText formats a slice of messages as readable text for summarization.
func FormatMessagesAsText(messages []MessageForSummary) string {
	var result string
	for _, msg := range messages {
		result += formatSingleMessage(msg)
		result += "\n\n"
	}
	return result
}

// MessageForSummary represents a simplified message for summarization.
type MessageForSummary struct {
	Role    string
	Content string
}

func formatSingleMessage(msg MessageForSummary) string {
	roleLabel := "User"
	if msg.Role == "assistant" {
		roleLabel = "Assistant"
	} else if msg.Role == "system" {
		roleLabel = "System"
	}
	return roleLabel + ":\n" + msg.Content
}
