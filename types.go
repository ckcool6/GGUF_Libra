package main

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// 用于在历史记录中存储图片的 Base64 数据
	// omitempty 表示如果没有图片，生成的 JSON 就不包含这个字段
	Image string `json:"image,omitempty"`

	Archives [][]Message `json:"archives,omitempty"`
	Abstract string      `json:"abstract,omitempty"`
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
	Message string `json:"message"`
	// 接收前端 JS 发来的图片 Base64
	Image              string `json:"image"`
	CustomUrl          string `json:"custom_url"`
	CustomKey          string `json:"custom_key"`
	CustomEmbeddingUrl string `json:"custom_embedding_url"`
}

type chatlist struct {
	ChatHistory  []Message
	UserMsgIndex int
	SendHistory  []Message
}

type LlamaContent struct {
	Type     string            `json:"type"`
	Text     string            `json:"text"`
	ImageURL *LlamaImageDetail `json:"image_url,omitempty"`
}

type LlamaImageDetail struct {
	URL string `json:"url"` // 格式: "data:image/jpeg;base64,xxxx"
}

type LlamaMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 这里可以是 string 或 []LlamaContent
}
