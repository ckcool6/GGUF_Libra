package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

	// 将侧线合并（Merge）回主线：将侧线摘要作为新节点插入主线末尾
	http.HandleFunc("/api/merge", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if currentChain == nil {
			http.Error(w, "当前节点为空", http.StatusBadRequest)
			return
		}

		// 检查是否有侧线可以合并
		if currentChain.DialogSide == nil {
			http.Error(w, "当前位置没有侧线分支", http.StatusBadRequest)
			return
		}

		// 检查侧线是否已经有了摘要 (前端应先调用 generate-abstract)
		if currentChain.DialogSide.DialogAbstract == "" {
			http.Error(w, "侧线尚未生成总结，请先生成总结再合并", http.StatusPreconditionFailed)
			return
		}

		// 执行合并逻辑
		// newNode 是合并后在主线末尾产生的那个携带摘要的新节点
		newNode, err := currentChain.Merge()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 重要：合并后，将当前的操作指针 currentChain 移动到这个新的主线节点上
		currentChain = newNode

		// 持久化保存
		rootChain.SaveChainToFile("chain_history.json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "已将侧线成果合并至主线末尾",
		})
	})

	//  手动触发生成当前节点的上下文摘要
	http.HandleFunc("/api/generate-abstract", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CustomUrl string `json:"custom_url"`
			CustomKey string `json:"custom_key"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.CustomKey == "" {
			authHeader := r.Header.Get("Authorization")
			body.CustomKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
		// 1. 快速读取当前节点指针后立即释放全局锁，不阻塞其他请求
		mu.Lock()
		targetChain := currentChain
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if targetChain != nil {
			// 2. 耗时的 AI 摘要生成过程在锁外并发执行，内部使用 chainMu 保证节点安全
			// 改为用两个变量接收返回值
			abstract, err := targetChain.GenerateAbstract(body.CustomUrl, body.CustomKey)

			// 如果发生了鉴权错误，直接返回 401 状态码给前端
			if err != nil && err.Error() == "AUTH_ERROR" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

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
