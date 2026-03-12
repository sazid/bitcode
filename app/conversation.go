package main

import (
	"sync"

	"github.com/openai/openai-go/v3"
)

type Conversation struct {
	mu       *sync.Mutex
	Messages []openai.ChatCompletionMessageParamUnion `json:"messages"`
	Tools    []openai.ChatCompletionToolUnionParam    `json:"tools"`
}

func NewConversation() *Conversation {
	return &Conversation{
		mu: &sync.Mutex{},
	}
}

func (c *Conversation) AddMessage(message openai.ChatCompletionMessageParamUnion) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = append(c.Messages, message)
}

func (c *Conversation) AddTool(tool openai.ChatCompletionToolUnionParam) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Tools = append(c.Tools, tool)
}
