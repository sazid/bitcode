package llm

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

type OpenAIProvider struct {
	client *openai.Client
}

func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))
	return &OpenAIProvider{client: &client}
}

func (p *OpenAIProvider) Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	if onDelta != nil {
		return p.completeStream(ctx, params, onDelta)
	}
	return p.completeSync(ctx, params)
}

func (p *OpenAIProvider) completeSync(ctx context.Context, params CompletionParams) (*CompletionResponse, error) {
	apiParams := buildOpenAIParams(params)

	resp, err := p.client.Chat.Completions.New(ctx, apiParams)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return responseFromChoice(resp.Choices[0]), nil
}

func (p *OpenAIProvider) completeStream(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	apiParams := buildOpenAIParams(params)

	stream := p.client.Chat.Completions.NewStreaming(ctx, apiParams)
	defer stream.Close()

	acc := openai.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				onDelta(StreamDelta{Type: DeltaText, Text: delta.Content})
			}
			// Stream tool call argument deltas
			for _, tc := range delta.ToolCalls {
				if tc.Function.Arguments != "" {
					onDelta(StreamDelta{
						Type:         DeltaToolArgs,
						Text:         tc.Function.Arguments,
						ToolCallID:   tc.ID,
						ToolCallName: tc.Function.Name,
					})
				}
				if tc.Function.Name != "" {
					onDelta(StreamDelta{
						Type:         DeltaToolName,
						ToolCallID:   tc.ID,
						ToolCallName: tc.Function.Name,
					})
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	if len(acc.Choices) == 0 {
		return nil, fmt.Errorf("no choices in streamed response")
	}

	return responseFromChoice(acc.Choices[0]), nil
}

func buildOpenAIParams(params CompletionParams) openai.ChatCompletionNewParams {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(params.Messages))
	for _, m := range params.Messages {
		msgs = append(msgs, toOpenAIMessage(m))
	}

	tools := make([]openai.ChatCompletionToolUnionParam, 0, len(params.Tools))
	for _, t := range params.Tools {
		tools = append(tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
			Parameters:  shared.FunctionParameters(t.Parameters),
		}))
	}

	p := openai.ChatCompletionNewParams{
		Model:    params.Model,
		Messages: msgs,
		Tools:    tools,
		N:        openai.Int(1),
	}
	if params.ReasoningEffort != "" {
		p.ReasoningEffort = openai.ReasoningEffort(params.ReasoningEffort)
	}
	if params.MaxTokens > 0 {
		p.MaxCompletionTokens = openai.Int(int64(params.MaxTokens))
	}
	return p
}

func responseFromChoice(choice openai.ChatCompletionChoice) *CompletionResponse {
	msg := Message{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: ContentText, Text: choice.Message.Content}},
	}
	for _, tc := range choice.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return &CompletionResponse{
		Message:      msg,
		FinishReason: FinishReason(choice.FinishReason),
	}
}

// hasMultiModalContent returns true if any content block is non-text.
func hasMultiModalContent(blocks []ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == ContentImage || b.Type == ContentAudio || b.Type == ContentDocument {
			return true
		}
	}
	return false
}

// toOpenAIContentParts converts content blocks to OpenAI content part union types.
func toOpenAIContentParts(blocks []ContentBlock) []openai.ChatCompletionContentPartUnionParam {
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case ContentText:
			parts = append(parts, openai.TextContentPart(b.Text))
		case ContentImage:
			if b.Source != nil {
				url := b.Source.URL
				if b.Source.Type == "base64" && b.Source.Data != "" {
					// Convert base64 to data URI for OpenAI
					url = "data:" + b.Source.MediaType + ";base64," + b.Source.Data
				}
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: url,
				}))
			}
		case ContentAudio:
			if b.Source != nil && b.Source.Data != "" {
				// Map our media type to OpenAI's expected format string
				format := "wav" // default
				switch b.Source.MediaType {
				case "audio/mp3", "audio/mpeg":
					format = "mp3"
				case "audio/wav":
					format = "wav"
				}
				parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   b.Source.Data,
					Format: format,
				}))
			}
		// ContentDocument, ContentVideo, ContentThinking — skip for OpenAI Chat Completions
		default:
			if b.Text != "" {
				parts = append(parts, openai.TextContentPart(b.Text))
			}
		}
	}
	return parts
}

func toOpenAIMessage(m Message) openai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case RoleSystem:
		return openai.SystemMessage(m.Text())
	case RoleUser:
		if hasMultiModalContent(m.Content) {
			parts := toOpenAIContentParts(m.Content)
			return openai.UserMessage[[]openai.ChatCompletionContentPartUnionParam](parts)
		}
		return openai.UserMessage(m.Text())
	case RoleTool:
		return openai.ToolMessage(m.Text(), m.ToolCallID)
	case RoleAssistant:
		text := m.Text()
		if len(m.ToolCalls) == 0 {
			return openai.AssistantMessage(text)
		}
		toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			toolCalls[i] = openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				},
			}
		}
		assistant := openai.ChatCompletionAssistantMessageParam{
			ToolCalls: toolCalls,
		}
		if text != "" {
			assistant.Content.OfString = param.NewOpt(text)
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
	}
	return openai.UserMessage(m.Text())
}
