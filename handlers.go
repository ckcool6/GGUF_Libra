package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 动态获取当前模型的 n_ctx 上限
func getLlamaMaxCtx(customURL string) int {
	apiURL := "http://127.0.0.1:8021/props"
	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			apiURL = fmt.Sprintf("%s://%s/props", parsedURL.Scheme, parsedURL.Host)
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return 512 // 拿不到时，使用 llama.cpp 的默认最小值 512 保底
	}
	defer resp.Body.Close()

	var propsData struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&propsData); err == nil && propsData.DefaultGenerationSettings.NCtx > 0 {
		return propsData.DefaultGenerationSettings.NCtx
	}

	return 512
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	var body reqBody

	if err := parse_input(r, w, &body); err != nil {
		return
	}

	load_history(globalId, &body)

	resp, err := sendRequestToLlama(r, &body, globalId.sendHistory)
	if err != nil {
		fmt.Println("❌ 无法连接到 llama.cpp 服务:", err)
		rollbackHistory(globalId, globalId.userMsgIndex)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "本地 API 请求失败"})
		return
	}
	defer resp.Body.Close()

	fmt.Println("llama.cpp 响应状态码:", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		rollbackHistory(globalId, globalId.userMsgIndex)
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ llama.cpp 报错返回: %s\n", string(bodyBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	fmt.Println("开始流式接收并转发数据...")
	aiFullContent, streamSuccess := forwardStreamData(w, r, resp.Body)

	if streamSuccess || aiFullContent != "" {
		fmt.Println("\n> 对话成功，保存历史记录。")
		mu.Lock()
		globalId.chatHistory = append(globalId.chatHistory, Message{Role: "assistant", Content: aiFullContent})
		saveHistoryToFile()
		mu.Unlock()
	} else {
		fmt.Println("\n 流传输异常中断且未获取到内容，执行回滚。")
		rollbackHistory(globalId, globalId.userMsgIndex)
	}
}

func load_history(cl *chatlist, body *reqBody) {
	mu.Lock()
	defer mu.Unlock()
	cl.chatHistory = append(cl.chatHistory, Message{Role: "user", Content: body.Message})
	cl.userMsgIndex = len(cl.chatHistory) - 1

	// 获取真实 n_ctx
	maxCtx := getLlamaMaxCtx(body.CustomUrl)

	// 动态计算预留空间：最多预留 2048，但如果总 context 较小，则预留 20% 的空间给 AI 输出
	reserveTokens := 2048
	if maxCtx/5 < reserveTokens {
		reserveTokens = maxCtx / 5 // 当 n_ctx 很小时，预留 20%
	}

	safeMaxTokens := maxCtx - reserveTokens
	if safeMaxTokens < 100 {
		safeMaxTokens = 100 // 保底值不能超过 maxCtx 本身
	}

	cl.sendHistory = filterMessagesByToken(cl.chatHistory, safeMaxTokens)
}

func rollbackHistory(cl *chatlist, index int) {
	mu.Lock()
	defer mu.Unlock()
	if index >= 0 && index < len(cl.chatHistory) {
		cl.chatHistory = append(cl.chatHistory[:index], cl.chatHistory[index+1:]...)
	}
}

func apiHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	json.NewEncoder(w).Encode(globalId.chatHistory)
}

// 动态清除 llama.cpp 指定 slot 的 KV 缓存
func eraseLlamaSlot(customURL string, slotID int) {
	apiURL := fmt.Sprintf("http://127.0.0.1:8021/slots/%d?action=erase", slotID)

	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			apiURL = fmt.Sprintf("%s://%s/slots/%d?action=erase", parsedURL.Scheme, parsedURL.Host, slotID)
		}
	}

	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		req, err := http.NewRequest("POST", apiURL, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
	}()
}

func apiNewChatHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	globalId.chatHistory = []Message{}
	saveHistoryToFile()
	mu.Unlock()

	// 获取前端传过来的 custom_url（如果有）
	customURL := r.URL.Query().Get("custom_url")

	// 动态清理 slot 0
	eraseLlamaSlot(customURL, 0)

	w.WriteHeader(http.StatusOK)
}

// 请求 llama.cpp 的 /props 接口获取真实的 context 占用
func apiLlamaPropsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	customURL := r.URL.Query().Get("custom_url")
	maxCtx := getLlamaMaxCtx(customURL)

	// 算当前实际聊天历史在使用的 Token 开销
	mu.Lock()
	filteredMsgs := filterMessagesByToken(globalId.chatHistory, maxCtx)
	currentTokens := 0
	for _, msg := range filteredMsgs {
		currentTokens += getMessageTokens(msg.Role, msg.Content)
	}
	mu.Unlock()

	responseData := map[string]interface{}{
		"default_generation_settings": map[string]interface{}{
			"n_ctx": maxCtx,
		},
		"slots": []map[string]interface{}{
			{
				"n_past": currentTokens,
			},
		},
	}

	json.NewEncoder(w).Encode(responseData)
}

// tool functions
func forwardStreamData(w http.ResponseWriter, r *http.Request, respBody io.ReadCloser) (string, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	reader := bufio.NewReader(respBody)

	var aiFullContent strings.Builder
	streamSuccess := false

Loop:
	for {
		select {
		case <-r.Context().Done():
			fmt.Println("\n🛑 检测到前端主动断开连接，停止接收流数据。")
			respBody.Close()
			break Loop
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Println("⚠️ 读取流时遭遇非 EOF 异常中断:", err)
			}
			break
		}

		if bytes.HasPrefix(line, []byte("data: ")) {
			if r.Context().Err() != nil {
				break
			}

			w.Write(line)
			w.Write([]byte("\n"))
			if flusher != nil {
				flusher.Flush()
			}

			data := bytes.TrimPrefix(line, []byte("data: "))
			data = bytes.TrimSpace(data)

			if bytes.Equal(data, []byte("[DONE]")) || bytes.Contains(data, []byte(`"done":true`)) {
				fmt.Println("\n> [DONE]")
				streamSuccess = true
				break
			}

			var streamResp struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &streamResp); err == nil && len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				aiFullContent.WriteString(content)
				fmt.Print(content)
			}
		}
	}

	io.Copy(io.Discard, respBody)
	return aiFullContent.String(), streamSuccess
}

func sendRequestToLlama(r *http.Request, body *reqBody, history []Message) (*http.Response, error) {
	payload := map[string]interface{}{
		"messages": history,
		"stream":   true,
	}
	jsonData, _ := json.Marshal(payload)

	apiURL := "http://127.0.0.1:8021/v1/chat/completions"
	if body.CustomUrl != "" {
		apiURL = body.CustomUrl
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if body.CustomKey != "" {
		req.Header.Set("Authorization", "Bearer "+body.CustomKey)
	}

	return (&http.Client{}).Do(req)
}

func parse_input(r *http.Request, w http.ResponseWriter, body *reqBody) error {
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fmt.Println("❌ 解析前端请求失败:", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return err
	}
	fmt.Println("> 用户输入:", body.Message)
	return nil
}

func apiGetPromptsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"active":  config.Active,
		"prompts": config.Prompts,
	})
}

func apiSwitchPromptHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	mu.Lock()
	success := setActivePrompt(body.ID)
	mu.Unlock()

	if !success {
		http.Error(w, "Prompt not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}
