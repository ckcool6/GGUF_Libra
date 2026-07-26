package main

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type SystemPromptConfig struct {
	Active  string       `json:"active"`
	Prompts []PromptItem `json:"prompts"`
}

type PromptItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type reqBody struct {
	Message   string `json:"message"`
	CustomUrl string `json:"custom_url"`
	CustomKey string `json:"custom_key"`
}

type chatlist struct {
	ChatHistory  []Message
	UserMsgIndex int
	SendHistory  []Message
}
