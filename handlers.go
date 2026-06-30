package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func chatHandler(w http.ResponseWriter, r *http.Request) {
	// 1. 解析前端输入
	var reqBody struct {
		Message   string `json:"message"`
		CustomUrl string `json:"custom_url"`
		CustomKey string `json:"custom_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		fmt.Println("❌ 解析前端请求失败:", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	fmt.Println("👤 用户输入:", reqBody.Message)

	// 2. 处理对话历史记录
	mu.Lock()
	chatHistory = append(chatHistory, Message{Role: "user", Content: reqBody.Message})
	userMsgIndex := len(chatHistory) - 1

	sendHistory := filterMessagesByToken(chatHistory, 4096)
	mu.Unlock()

	// 3. 构建给 llama.cpp 的请求体 (移除特定的 model 限制，适配本地服务)
	payload := map[string]interface{}{
		"messages": sendHistory,
		"stream":   true,
	}
	jsonData, _ := json.Marshal(payload)

	// 4. 创建请求 (请确保这里的端口和路径与你的 llama.cpp 启动参数一致)
	// 判断是否使用用户前端填写的自定义地址
	apiURL := "http://127.0.0.1:8021/v1/chat/completions" // 默认本地地址
	if reqBody.CustomUrl != "" {
		apiURL = reqBody.CustomUrl
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("❌ 创建请求对象失败:", err)
		rollbackHistory(userMsgIndex)
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}

	// 判断是否使用用户前端填写的自定义 Key
	apiKey := OpenRouterKey // 默认环境变量 Key
	if reqBody.CustomKey != "" {
		apiKey = reqBody.CustomKey
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 5. 发送请求给 llama.cpp
	fmt.Println("🚀 正在请求 llama.cpp...")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		fmt.Println("❌ 无法连接到 llama.cpp 服务:", err)
		rollbackHistory(userMsgIndex)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "本地 API 请求失败"})
		return
	}
	defer resp.Body.Close()

	fmt.Println("📥 llama.cpp 响应状态码:", resp.StatusCode)

	// 6. 检查状态码
	if resp.StatusCode != http.StatusOK {
		rollbackHistory(userMsgIndex)
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ llama.cpp 报错返回: %s\n", string(bodyBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	// 7. 准备向前端流式输出
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		fmt.Println("⚠️ 当前环境不支持 Flusher")
	}

	fmt.Println("🌊 开始流式接收并转发数据...")
	reader := bufio.NewReader(resp.Body)
	var aiFullContent string
	streamSuccess := false // 用于标记流是否完整结束

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Println("⚠️ 读取流时遭遇非 EOF 异常中断:", err)
			}
			break
		}

		// 打印每一行原始数据，方便在控制台 debug 格式
		// fmt.Printf("原始行: %s", string(line))

		if bytes.HasPrefix(line, []byte("data: ")) {
			// 直接转发给前端
			w.Write(line)
			w.Write([]byte("\n"))
			if flusher != nil {
				flusher.Flush()
			}

			data := bytes.TrimPrefix(line, []byte("data: "))
			data = bytes.TrimSpace(data)

			// 兼容不同版本的 llama.cpp 结束符判断
			if bytes.Equal(data, []byte("[DONE]")) || bytes.Contains(data, []byte(`"done":true`)) {
				fmt.Println("\n✅ 收到完整结束信号 [DONE]")
				streamSuccess = true
				break
			}

			// 解析内容用于后端历史记录存储
			var streamResp struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &streamResp); err == nil && len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				aiFullContent += content
				fmt.Print(content) // 在终端实时打印 AI 的回复
			}
		}
	}

	// 8. 最终状态判定与持久化
	// 如果正常读完，或者虽然没读到 DONE 但好歹吐出了一点东西，就认为成功
	if streamSuccess || aiFullContent != "" {
		fmt.Println("\n💾 对话成功，保存历史记录。")
		mu.Lock()
		chatHistory = append(chatHistory, Message{Role: "assistant", Content: aiFullContent})
		saveHistoryToFile()
		mu.Unlock()
	} else {
		fmt.Println("\n🚨 流传输异常中断且未获取到内容，执行回滚。")
		rollbackHistory(userMsgIndex)
	}
}

// ✨【新增辅助函数】用于安全回滚历史记录，避免队列被 user 消息污染
func rollbackHistory(index int) {
	mu.Lock()
	defer mu.Unlock()
	if index >= 0 && index < len(chatHistory) {
		// 移除触发报错的那条 user 消息
		chatHistory = append(chatHistory[:index], chatHistory[index+1:]...)
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
