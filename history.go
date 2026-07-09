package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkoukk/tiktoken-go"
)

var (
	//chatHistory  []Message
	tkm          *tiktoken.Tiktoken
	systemPrompt Message
	config       SystemPromptConfig
)

func config_init() {
	var err error

	stopLoading := StartLoading("正在初始化 Token 编码器（如果是首次运行，可能需要下载词表文件，请稍候）...")

	// 初始化 Token 编码器
	tkm, err = tiktoken.GetEncoding("cl100k_base")

	close(stopLoading)

	if err != nil {
		panic(fmt.Sprintf("初始化 Token 编码器失败: %v", err))
	}

	// 从 JSON 文件加载 System Prompt
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

	systemPrompt = Message{
		Role:    "system",
		Content: config.Content,
	}
	return nil
}

// 计算单条消息的大致 Token 数（直接使用全局变量 tkm）
func getMessageTokens(role, content string) int {
	// 基础开销：每条消息大约有 4 个 token 的元数据开销 ({role, content})
	tokens := 4
	tokens += len(tkm.Encode(role, nil, nil))
	tokens += len(tkm.Encode(content, nil, nil))
	return tokens
}

// 核心裁剪函数：从后往前取，直到达到 maxTokens
func filterMessagesByToken(history []Message, maxTokens int) []Message {
	var result []Message
	totalTokens := 0

	// 加上 System Prompt 的 Token 开销
	totalTokens += getMessageTokens(systemPrompt.Role, systemPrompt.Content)

	if totalTokens > maxTokens {
		return []Message{systemPrompt}
	}

	// 从最新的消息开始往前遍历
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

	// 双指针reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return append([]Message{systemPrompt}, result...)
}

func saveHistoryToFile() {
	data, _ := json.MarshalIndent(globalId.chatHistory, "", "  ")
	_ = os.WriteFile("history.json", data, 0644)
}

// main.go init
func loadHistoryFromFile() {
	data, err := os.ReadFile("history.json")
	if err == nil {
		json.Unmarshal(data, &globalId.chatHistory)
		fmt.Println("已从 history.json 恢复对话记录")
	}
}
