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

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			onDelta(StreamDelta{Text: chunk.Choices[0].Delta.Content})
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

	return openai.ChatCompletionNewParams{
		Model:           params.Model,
		Messages:        msgs,
		Tools:           tools,
		ReasoningEffort: openai.ReasoningEffort(params.ReasoningEffort),
		N:               openai.Int(1),
	}
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

func toOpenAIMessage(m Message) openai.ChatCompletionMessageParamUnion {
	text := m.Text()

	switch m.Role {
	case RoleSystem:
		return openai.SystemMessage(text)
	case RoleUser:
		return openai.UserMessage(text)
	case RoleTool:
		return openai.ToolMessage(text, m.ToolCallID)
	case RoleAssistant:
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
	return openai.UserMessage(text)
}
