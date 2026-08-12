package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// 动态获取当前模型的 n_ctx 上限
func getLlamaMaxCtx(customURL string) int {
	apiURL := "http://127.0.0.1:8021/props"
	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			apiURL = fmt.Sprintf("%s://%s/props", parsedURL.Scheme, parsedURL.Host)
		}
	}

	// 使用全局复用的 httpTimeoutClient 发起请求
	resp, err := httpTimeoutClient.Get(apiURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return 512 // 拿不到时，使用 llama.cpp 的默认最小值 512 保底
	}
	defer resp.Body.Close()

	var propsData struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}

	// 解析响应 JSON
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

	// 1. 获取本地局部引用，立刻释放全局锁
	mu.Lock()
	localChain := currentChain
	mu.Unlock()

	if localChain == nil {
		http.Error(w, "No active chain", http.StatusBadRequest)
		return
	}

	// 2. 载入历史记录
	load_history(localChain, &body)

	// 3. 网络请求完全无锁运行
	resp, err := sendRequestToLlama(r, &body, localChain.DialogContent.SendHistory)
	if err != nil {
		fmt.Println("❌ 无法连接到 llama.cpp 服务:", err)
		rollbackHistory(localChain)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "本地 API 请求失败"})
		return
	}
	defer resp.Body.Close()

	// 如果下游（Nginx/llamacpp）返回 401，立刻回滚历史并传给前端
	if resp.StatusCode == http.StatusUnauthorized {
		rollbackHistory(localChain)
		w.WriteHeader(http.StatusUnauthorized) // 发送 401，让前端触发“鉴权失败”提示
		return
	}

	// 4. 流式传输与后续更新
	aiFullContent, streamSuccess := forwardStreamData(w, r, resp.Body)

	if streamSuccess || aiFullContent != "" {
		// 追加 AI 回复文本到历史记录
		localChain.DialogContent.ChatHistory = append(localChain.DialogContent.ChatHistory, Message{
			Role:    "assistant",
			Content: aiFullContent,
		})

		// 写文件时用全局锁守护整棵树的序列化
		mu.Lock()
		rootChain.SaveChainToFile("chain_history.json")
		mu.Unlock()
	} else {
		rollbackHistory(localChain)
	}
}

func load_history(chain *chatChain, body *reqBody) {
	mu.Lock()
	defer mu.Unlock()

	if chain == nil || chain.DialogContent == nil {
		return
	}

	// 存入消息时必须包含 Image
	chain.DialogContent.ChatHistory = append(chain.DialogContent.ChatHistory, Message{
		Role:    "user",
		Content: body.Message,
		Image:   body.Image, // 必须把图片 Base64 存入历史
	})
	chain.DialogContent.UserMsgIndex = len(chain.DialogContent.ChatHistory) - 1

	// 计算当前节点发送给模型时的 safeMaxTokens
	maxCtx := getLlamaMaxCtx(body.CustomUrl)

	reserveTokens := 2048
	if maxCtx/5 < reserveTokens {
		reserveTokens = maxCtx / 5
	}

	safeMaxTokens := maxCtx - reserveTokens
	if safeMaxTokens < 100 {
		safeMaxTokens = 100
	}

	// 过滤消息填入 SendHistory
	chain.DialogContent.SendHistory = filterMessagesByToken(chain.DialogContent.ChatHistory, safeMaxTokens)
}

func rollbackHistory(chain *chatChain) {
	mu.Lock()
	defer mu.Unlock()

	if chain == nil || chain.DialogContent == nil {
		return
	}

	idx := chain.DialogContent.UserMsgIndex
	if idx >= 0 && idx < len(chain.DialogContent.ChatHistory) {
		chain.DialogContent.ChatHistory = append(chain.DialogContent.ChatHistory[:idx], chain.DialogContent.ChatHistory[idx+1:]...)
	}
}

func apiHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile("chain_history.json")
	if err != nil {
		json.NewEncoder(w).Encode([]Message{})
		return
	}

	var root chatChain
	if err := json.Unmarshal(data, &root); err != nil {
		http.Error(w, "解析历史 JSON 失败", http.StatusInternalServerError)
		return
	}

	history := []Message{}

	// 深度遍历整棵树（主线 + 侧线），按顺序搜集所有非 system 消息
	var collectMessages func(node *chatChain)
	collectMessages = func(node *chatChain) {
		if node == nil {
			return
		}

		if node.DialogContent != nil {
			for _, msg := range node.DialogContent.ChatHistory {
				if msg.Role != "system" {
					history = append(history, msg)
				}
			}
		}

		// 依次搜集主线和侧线
		collectMessages(node.DialogMain)
		collectMessages(node.DialogSide)
	}

	collectMessages(&root)

	json.NewEncoder(w).Encode(history)
}

// 辅助方法：安全获取当前节点的 ChatHistory
func (chain *chatChain) dialogChainContentOrDefault() []Message {
	if chain == nil || chain.DialogContent == nil {
		return []Message{}
	}
	return chain.DialogContent.ChatHistory
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
		req, err := http.NewRequest("POST", apiURL, nil)
		if err != nil {
			return
		}

		// 使用全局复用的 httpTimeoutClient 发起异步清理请求
		resp, err := httpTimeoutClient.Do(req)
		if err != nil {
			return
		}

		// 确保把 Body 读完并关闭，TCP 连接才能被 client 正确回收重用
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

func apiNewChatHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	// 1. 在内存中彻底创建一个全新的干净树节点
	rootChain = NewChatChain()
	currentChain = rootChain

	// 2. 将这棵空树覆盖写入 chain_history.json 文件
	rootChain.SaveChainToFile("chain_history.json")
	mu.Unlock()

	// 3. 动态清理 llama.cpp 的 slot 0 缓存
	customURL := r.URL.Query().Get("custom_url")
	eraseLlamaSlot(customURL, 0)

	w.WriteHeader(http.StatusOK)
}

// 请求 llama.cpp 的 /props 接口获取真实的 context 占用
func apiLlamaPropsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	customURL := r.URL.Query().Get("custom_url")
	maxCtx := getLlamaMaxCtx(customURL)

	// 计算 currentChain 当前实际聊天历史在使用的 Token 开销
	mu.Lock()
	currentTokens := 0
	if currentChain != nil && currentChain.DialogContent != nil {
		// 扣掉预留给 AI 输出的空间
		reserveTokens := 2048
		if maxCtx/5 < reserveTokens {
			reserveTokens = maxCtx / 5
		}
		safeMaxTokens := maxCtx - reserveTokens
		if safeMaxTokens < 100 {
			safeMaxTokens = 100
		}

		// 用 safeMaxTokens 来裁剪计算，这样算出来的就是“真正会发给 AI 的有效上下文 Token”
		filteredMsgs := filterMessagesByToken(currentChain.DialogContent.ChatHistory, safeMaxTokens)
		for _, msg := range filteredMsgs {
			currentTokens += getMessageTokens(msg)
		}
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
	formattedMessages := make([]LlamaMessage, 0, len(history))

	for _, m := range history {
		if m.Image != "" {
			// 有图片，组装为 []LlamaContent 数组
			contentArray := []LlamaContent{
				{Type: "text", Text: m.Content},
				{
					Type: "image_url",
					ImageURL: &LlamaImageDetail{
						URL: "data:image/jpeg;base64," + m.Image,
					},
				},
			}
			formattedMessages = append(formattedMessages, LlamaMessage{
				Role:    m.Role,
				Content: contentArray, // interface{} 可以接收 slice
			})
		} else {
			// 没图片，直接用字符串
			formattedMessages = append(formattedMessages, LlamaMessage{
				Role:    m.Role,
				Content: m.Content, // interface{} 可以接收 string
			})
		}
	}

	payload := map[string]interface{}{
		"messages": formattedMessages,
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

	// 优先尝试从 JSON Body 获取
	if body.CustomKey != "" {
		req.Header.Set("Authorization", "Bearer "+body.CustomKey)
	} else {
		// 如果 Body 里没传，则直接转发前端发给 Go 的 Authorization Header
		// 这就是你前端 getHeaders() 函数发送的内容： "Bearer your_key"
		if auth := r.Header.Get("Authorization"); auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}

	return httpClient.Do(req)
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
