package llm

// EstimateTokens guesses how many tokens a text costs, for budgeting a
// prompt against a context window before sending it. It is not a
// tokenizer: four characters per token for Latin text and one and a half
// per CJK character is close enough to keep a request under a window with
// the margin the callers leave, and costs nothing.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	latin := 0
	wide := 0
	for _, character := range text {
		if character >= 0x2E80 {
			wide++
		} else {
			latin++
		}
	}
	return (latin+3)/4 + (wide*3+1)/2
}

// EstimateMessagesTokens sums the estimate over a conversation, charging a
// few tokens per message for the framing every API adds.
func EstimateMessagesTokens(messages []ChatMessage) int {
	total := 0
	for _, message := range messages {
		total += 4 + EstimateTokens(message.Text())
		for _, call := range message.ToolCalls {
			total += 8 + EstimateTokens(call.Name) + EstimateTokens(call.Arguments)
		}
		for _, part := range message.Parts {
			if part.Type == "image" {
				total += 1000
			}
		}
	}
	return total
}

// EstimateToolsTokens sums the estimate over tool definitions, which are
// sent with every round and are the bulk of a large catalog.
func EstimateToolsTokens(tools []ToolDefinition) int {
	total := 0
	for _, tool := range tools {
		total += 12 + EstimateTokens(tool.Name) + EstimateTokens(tool.Description) + estimateSchemaTokens(tool.Parameters)
	}
	return total
}

func estimateSchemaTokens(schema map[string]any) int {
	total := 0
	for key, value := range schema {
		total += EstimateTokens(key) + 2
		switch typed := value.(type) {
		case string:
			total += EstimateTokens(typed)
		case map[string]any:
			total += estimateSchemaTokens(typed)
		case []any:
			for _, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					total += estimateSchemaTokens(nested)
				} else if text, ok := item.(string); ok {
					total += EstimateTokens(text)
				}
			}
		case []string:
			for _, item := range typed {
				total += EstimateTokens(item)
			}
		}
	}
	return total
}
