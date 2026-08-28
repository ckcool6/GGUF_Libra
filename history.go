package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tkm          *tiktoken.Tiktoken
	systemPrompt Message
	config       SystemPromptConfig
)

func config_init() {
	var err error

	// 尝试从本地加载已保存的树状历史
	rootChain, err = LoadChainFromFile("chain_history.json")
	if err != nil || rootChain == nil {
		fmt.Println("未找到历史链文件，初始化新链...")
		rootChain = NewChatChain()
	} else {
		fmt.Println("成功加载历史链结构")
	}

	// 默认将 currentChain 指向主线最深处的末尾节点
	currentChain = rootChain
	for currentChain.DialogMain != nil {
		currentChain = currentChain.DialogMain
	}

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

func getMessageTokens(msg Message) int {
	// 基础 Token (Role + Content)
	tokens := 4
	tokens += len(tkm.Encode(msg.Role, nil, nil))
	tokens += len(tkm.Encode(msg.Content, nil, nil))

	//  图片 Token
	if msg.Image != "" {
		// 图片经过 mmproj 处理后会占用固定的视觉 Token 槽位。
		// 1024 是一个比较通用的保守估算值。
		tokens += 1024
	}
	return tokens
}

func filterMessagesByToken(history []Message, maxTokens int) []Message {
	var result []Message
	var systemBackdrops []Message // 专门存放归档背景
	totalTokens := 0

	// 1. 计算全局系统提示词 (人设)
	if systemPrompt.Content != "" {
		totalTokens += getMessageTokens(systemPrompt)
	}

	// 2. 预处理：先从历史中找出所有的“归档背景” (system 角色且包含关键字)
	// 这些是灵魂，必须优先保送
	for _, msg := range history {
		if msg.Role == "system" && (strings.Contains(msg.Content, "前情提要") || strings.Contains(msg.Content, "上下文")) {
			tokens := getMessageTokens(msg)
			if totalTokens+tokens <= maxTokens {
				totalTokens += tokens
				systemBackdrops = append(systemBackdrops, msg)
			}
		}
	}

	// 3. 倒序处理常规对话 (user / assistant)
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]

		// 如果是系统消息，我们已经在上面处理过了，这里跳过
		if msg.Role == "system" {
			continue
		}

		msgTokens := getMessageTokens(msg)

		if totalTokens+msgTokens > maxTokens {
			break // 达到 Token 上限，停止拉取更旧的消息
		}

		totalTokens += msgTokens
		result = append(result, msg)
	}

	// 4. 反转对话顺序（因为上面是倒序拉取的）
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	// 5. 最终组装：[全局人设] + [历史背景摘要] + [近期对话记录]
	finalMessages := []Message{}
	if systemPrompt.Content != "" {
		finalMessages = append(finalMessages, systemPrompt)
	}
	finalMessages = append(finalMessages, systemBackdrops...)
	finalMessages = append(finalMessages, result...)

	return finalMessages
}
