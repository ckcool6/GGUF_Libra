package main

// 定义消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SystemPromptConfig 对应 system_prompt.json 的结构
type SystemPromptConfig struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
}
