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

	cl.sendHistory = filterMessagesByToken(cl.chatHistory, 4096)
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

func apiNewChatHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	globalId.chatHistory = []Message{}
	saveHistoryToFile()
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// tool functions
func forwardStreamData(w http.ResponseWriter, r *http.Request, respBody io.ReadCloser) (string, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	reader := bufio.NewReader(respBody)
	var aiFullContent string
	streamSuccess := false

Loop:
	for {
		select {
		case <-r.Context().Done():
			fmt.Println("\n🛑 检测到前端主动断开连接，停止接收流数据。")
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
				Model   string `json:"model"` // 拦截大模型的名字
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &streamResp); err == nil && len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				aiFullContent += content
				fmt.Print(content)
			}
		}
	}

	io.Copy(io.Discard, respBody)
	return aiFullContent, streamSuccess
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
