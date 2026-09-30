package main

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Base64-encoded image data stored in history.
	// omitempty omits this field from the generated JSON if no image is present.
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
	// Receives the base64-encoded image sent from the frontend.
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
	URL string `json:"url"` // Format: "data:image/jpeg;base64,xxxx"
}

type LlamaMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be either a string or []LlamaContent.

}
