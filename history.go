package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tkm          *tiktoken.Tiktoken
	systemPrompt Message
	config       SystemPromptConfig
)

func config_init() {
	var err error

	stopLoading := StartLoading("正在初始化 Token 编码器（如果是首次运行，可能需要下载词表文件，请稍候）...")

	tkm, err = tiktoken.GetEncoding("cl100k_base")

	close(stopLoading)

	if err != nil {
		panic(fmt.Sprintf("初始化 Token 编码器失败: %v", err))
	}

	err = loadSystemPrompt("system_prompt.json")
	if err != nil {
		panic(fmt.Sprintf("加载 System Prompt 失败: %v", err))
	}
	fmt.Println()
	fmt.Printf("【系统】成功加载提示词配置文件\n")
}

func loadSystemPrompt(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	setActivePrompt(config.Active)
	return nil
}

func setActivePrompt(id string) bool {
	for _, p := range config.Prompts {
		if p.ID == id {
			config.Active = id
			systemPrompt = Message{
				Role:    "system",
				Content: p.Content,
			}
			return true
		}
	}
	return false
}

func getMessageTokens(role, content string) int {
	tokens := 4
	tokens += len(tkm.Encode(role, nil, nil))
	tokens += len(tkm.Encode(content, nil, nil))
	return tokens
}

func filterMessagesByToken(history []Message, maxTokens int) []Message {
	var result []Message
	totalTokens := 0

	if systemPrompt.Content != "" {
		totalTokens += getMessageTokens(systemPrompt.Role, systemPrompt.Content)
	}

	if totalTokens > maxTokens {
		return []Message{systemPrompt}
	}

	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]

		if msg.Role == "system" {
			continue
		}

		msgTokens := getMessageTokens(msg.Role, msg.Content)

		if totalTokens+msgTokens > maxTokens {
			break
		}

		totalTokens += msgTokens
		result = append(result, msg)
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	if systemPrompt.Content != "" {
		return append([]Message{systemPrompt}, result...)
	}
	return result
}
