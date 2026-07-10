package main

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type SystemPromptConfig struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
}

type reqBody struct {
	Message   string `json:"message"`
	CustomUrl string `json:"custom_url"`
	CustomKey string `json:"custom_key"`
}

type chatlist struct {
	chatHistory  []Message
	userMsgIndex int
	sendHistory  []Message
}
