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
	// 修改 /api/merge 路由
	http.HandleFunc("/api/merge", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if rootChain == nil || currentChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "对话未初始化"})
			return
		}

		// 执行合并逻辑
		// 传入 rootChain 是因为需要它来做 DFS 路径搜索
		// 传入 currentChain 是因为它是侧线的终点，承载着摘要
		newNode, err := rootChain.Merge(currentChain)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// 这一步非常关键：合并后，将用户的操作指针指回主线的新节点
		currentChain = newNode

		// 保存状态到文件
		rootChain.SaveChainToFile("chain_history.json")

		w.Header().Set("Content-Type", "application/json")
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

	http.HandleFunc("/api/discard-side", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if rootChain == nil || currentChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 1. 寻找该侧线是从主线哪个点分出来的
		forkNode := rootChain.BackToMainForkedNode(currentChain)

		// 如果找不到分叉点，说明已经在主线上了，或者树结构异常
		if forkNode == nil {
			// 保险起见，直接把 currentChain 指向主线末尾
			currentChain = rootChain
			for currentChain.DialogMain != nil {
				currentChain = currentChain.DialogMain
			}
		} else {
			// 2. 【物理删除】直接将分叉点的侧线指针设为 nil
			// 这样整个聊烂了的侧线子树都会被 Go 的 GC 回收，且不会存入 JSON
			forkNode.DialogSide = nil

			// 3. 【回归正史】寻找主线现在的最末尾
			mainTail := rootChain
			for mainTail.DialogMain != nil {
				mainTail = mainTail.DialogMain
			}
			currentChain = mainTail
		}

		// 保存状态，文件里的侧线历史会瞬间消失
		rootChain.SaveChainToFile("chain_history.json")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "侧线已丢弃，已回到主线末尾",
		})
	})

	// 修改摘要的接口
	http.HandleFunc("/api/edit-abstract", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Abstract string `json:"abstract"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if currentChain != nil {
			currentChain.EditAbstract(body.Abstract)
			// 修改完立刻落盘，防止丢失
			rootChain.SaveChainToFile("chain_history.json")

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}

		http.Error(w, "节点不存在", http.StatusNotFound)
	})

	fmt.Println("服务已启动，请在浏览器中打开: http://127.0.0.1:8099")
	http.ListenAndServe(":8099", nil)
}
