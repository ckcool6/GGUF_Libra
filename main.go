package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	"net/http"
	"os"
	"sync"
)

import "github.com/joho/godotenv"

var (
	mu sync.Mutex
)

// 定义消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// 内存中的对话历史
var chatHistory []Message

var OpenRouterKey string

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
	systemPrompt := Message{Role: "system", Content: "你是一个简洁的助手。"}
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

func main() {
	godotenv.Load() // 自动读取 .env 文件并加载到环境变量
	OpenRouterKey = os.Getenv("OPENROUTER_KEY")

	loadHistoryFromFile() // 启动即加载

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// 聊天接口
	http.HandleFunc("/api/chat", chatHandler)

	http.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "manifest.json")
	})

	http.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "sw.js")
	})

	http.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "icon.png")
	})

	// 1. 修改 /api/history 路由
	http.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()         // 加锁
		defer mu.Unlock() // 确保函数结束解锁
		json.NewEncoder(w).Encode(chatHistory)
	})

	// 2. 修改 /api/new-chat 路由
	http.HandleFunc("/api/new-chat", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock() // 加锁
		chatHistory = []Message{}
		saveHistoryToFile()
		mu.Unlock() // 解锁
		w.WriteHeader(http.StatusOK)
	})

	// 3. 修改 /api/delete-last 路由
	http.HandleFunc("/api/delete-last", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock() // 加锁
		if len(chatHistory) >= 2 {
			chatHistory = chatHistory[:len(chatHistory)-2]
			saveHistoryToFile()
		}
		mu.Unlock() // 解锁
		w.WriteHeader(http.StatusOK)
	})

	fmt.Println("服务已启动: http://0.0.0.0:8024")
	http.ListenAndServe(":8024", nil)
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		return
	}

	// 1. 加锁并处理对话历史
	mu.Lock()
	// 将用户输入加入历史
	chatHistory = append(chatHistory, Message{Role: "user", Content: reqBody.Message})
	// 裁剪用于发送给 API 的上下文 (4000 Tokens)
	sendHistory := filterMessagesByToken(chatHistory, 4000)
	mu.Unlock()

	// 2. 准备流式响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	// 3. 构建请求
	payload := map[string]interface{}{
		"model":    "x-ai/grok-4.1-fast",
		"messages": sendHistory,
		"stream":   true,
		"reasoning": map[string]interface{}{
			"effort":     "medium", // 降低思考成本
			"max_tokens": 1000,     // 封顶思考字数
		},
	}
	jsonData, _ := json.Marshal(payload)

	// 建议增加超时控制
	req, _ := http.NewRequestWithContext(r.Context(), "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+OpenRouterKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	var aiFullContent string

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}

		if bytes.HasPrefix(line, []byte("data: ")) {
			w.Write(line)
			w.Write([]byte("\n"))
			flusher.Flush()

			data := bytes.TrimPrefix(line, []byte("data: "))
			if bytes.Contains(data, []byte("[DONE]")) {
				break
			}

			var streamResp struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &streamResp); err == nil && len(streamResp.Choices) > 0 {
				aiFullContent += streamResp.Choices[0].Delta.Content
			}
		}
	}

	// 4. 将 AI 回复存入历史并持久化
	if aiFullContent != "" {
		mu.Lock()
		chatHistory = append(chatHistory, Message{Role: "assistant", Content: aiFullContent})
		saveHistoryToFile()
		mu.Unlock()
	}
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
