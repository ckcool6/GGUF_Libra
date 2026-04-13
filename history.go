package main

import (
	"encoding/json"
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	"os"
)

// 内存中的对话历史
var chatHistory []Message

// 计算单条消息的大致 Token 数
func getMessageTokens(encoding *tiktoken.Tiktoken, role, content string) int {
	// 基础开销：每条消息大约有 4 个 token 的元数据开销 ({role, content})
	tokens := 4
	tokens += len(encoding.Encode(role, nil, nil))
	tokens += len(encoding.Encode(content, nil, nil))
	return tokens
}

// 核心裁剪函数：从后往前取，直到达到 maxTokens
func filterMessagesByToken(history []Message, maxTokens int) []Message {
	// 获取用于 cl100k_base (GPT-4/Grok) 的编码器
	tkm, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		fmt.Println("Encoding error:", err)
		return history // 降级处理：出错则返回原样
	}

	var result []Message
	totalTokens := 0

	// 预留固定 Token 给 System Prompt (假设 50)
	systemPrompt := Message{Role: "system", Content: "你是一个可爱的助手。"}
	totalTokens += getMessageTokens(tkm, systemPrompt.Role, systemPrompt.Content)

	// 从最新的消息开始往前遍历
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		msgTokens := getMessageTokens(tkm, msg.Role, msg.Content)

		if totalTokens+msgTokens > maxTokens {
			break // 超过阈值，停止收集
		}

		totalTokens += msgTokens
		// 插入到结果数组的最前面
		result = append([]Message{msg}, result...)
	}

	// 最终组合：System Prompt + 裁剪后的历史
	return append([]Message{systemPrompt}, result...)
}

// 将内存中的历史记录保存到磁盘
func saveHistoryToFile() {
	data, _ := json.MarshalIndent(chatHistory, "", "  ")
	_ = os.WriteFile("history.json", data, 0644)
}

// 在 main 函数启动时调用：从磁盘加载旧记录
func loadHistoryFromFile() {
	data, err := os.ReadFile("history.json")
	if err == nil {
		json.Unmarshal(data, &chatHistory)
		fmt.Println("已从 history.json 恢复对话记录")
	}
}
