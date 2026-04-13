package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
)

func chatHandler(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		return
	}

	// 加锁并处理对话历史
	mu.Lock()
	// 将用户输入加入历史
	chatHistory = append(chatHistory, Message{Role: "user", Content: reqBody.Message})
	// 裁剪用于发送给 API 的上下文 (4000 Tokens)
	sendHistory := filterMessagesByToken(chatHistory, 4000)
	mu.Unlock()

	// 构建请求
	payload := map[string]interface{}{
		"model":    "x-ai/grok-4.1-fast",
		"messages": sendHistory,
		"stream":   true,
		/* "reasoning": map[string]interface{}{
			"effort":     "medium", // 降低思考成本
			"max_tokens": 1000,     // 封顶思考字数
		}, */
	}
	jsonData, _ := json.Marshal(payload)

	// 建议增加超时控制
	req, _ := http.NewRequestWithContext(r.Context(), "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+OpenRouterKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "http://localhost:8024") // 你的项目地址
	req.Header.Set("X-Title", "MyGrokBotV1")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "API 请求失败"})
		return
	}
	defer resp.Body.Close()

	// --- 只有在成功获取 API 响应后，才宣告我们要开始流式传输了 ---
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

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

	// 将 AI 回复存入历史并持久化
	if aiFullContent != "" {
		mu.Lock()
		chatHistory = append(chatHistory, Message{Role: "assistant", Content: aiFullContent})
		saveHistoryToFile()
		mu.Unlock()
	}
}

func apiHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()         // 加锁
	defer mu.Unlock() // 确保函数结束解锁
	json.NewEncoder(w).Encode(chatHistory)
}

func apiNewChatHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock() // 加锁
	chatHistory = []Message{}
	saveHistoryToFile()
	mu.Unlock() // 解锁
	w.WriteHeader(http.StatusOK)
}
