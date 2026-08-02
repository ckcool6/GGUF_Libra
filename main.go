package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
	rootChain     *chatChain
	currentChain  *chatChain
)

var (
	// 用于长连接/流式对话请求
	httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// 用于常规超时请求（如生成摘要、获取 props）
	httpTimeoutClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		},
	}
)

func main() {

	config_init()

	// 托管整个 dist 目录
	http.Handle("/", http.FileServer(http.Dir("dist")))

	// 聊天接口
	http.HandleFunc("/api/chat", chatHandler)

	// 修改 /api/history 路由
	http.HandleFunc("/api/history", apiHistoryHandler)

	// 修改 /api/new-chat 路由
	http.HandleFunc("/api/new-chat", apiNewChatHandler)

	// 获取ctx
	http.HandleFunc("/api/llama-props", apiLlamaPropsHandler)

	// switch prompt
	http.HandleFunc("/api/prompts", apiGetPromptsHandler)
	http.HandleFunc("/api/switch-prompt", apiSwitchPromptHandler)

	// 修改 /api/delete-last 路由
	http.HandleFunc("/api/delete-last", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if currentChain != nil && currentChain.DialogContent != nil {
			history := currentChain.DialogContent.ChatHistory
			if len(history) >= 2 {
				currentChain.DialogContent.ChatHistory = history[:len(history)-2]
				rootChain.SaveChainToFile("chain_history.json")
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// 生成侧线分支（Fork）
	http.HandleFunc("/api/fork-side", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if currentChain != nil {
			// 生成侧线子节点，并将 currentChain 指向新侧线
			currentChain = currentChain.AppendSideBranchNode()
			rootChain.SaveChainToFile("chain_history.json")
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// 将侧线变基（Rebase）合并回主线
	http.HandleFunc("/api/rebase-side", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if currentChain != nil {
			currentChain.RebaseAllSideToMain()
			rootChain.SaveChainToFile("chain_history.json")
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	//  手动触发生成当前节点的上下文摘要
	http.HandleFunc("/api/generate-abstract", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CustomUrl string `json:"custom_url"`
			CustomKey string `json:"custom_key"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		// 1. 快速读取当前节点指针后立即释放全局锁，不阻塞其他请求
		mu.Lock()
		targetChain := currentChain
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if targetChain != nil {
			// 2. 耗时的 AI 摘要生成过程在锁外并发执行，内部使用 chainMu 保证节点安全
			abstract := targetChain.GenerateAbstract(body.CustomUrl, body.CustomKey)

			// 3. 摘要生成完毕后再快速持锁落盘
			mu.Lock()
			rootChain.SaveChainToFile("chain_history.json")
			mu.Unlock()

			json.NewEncoder(w).Encode(map[string]string{
				"abstract": abstract,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"abstract": ""})
	})

	fmt.Println("服务已启动，请在浏览器中打开: http://127.0.0.1:8099")
	http.ListenAndServe(":8099", nil)
}
