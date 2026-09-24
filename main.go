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
		Timeout: 600 * time.Second,
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

		mu.Lock()
		targetChain := currentChain
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		// 如果节点根本不存在，直接抛出 400 错误，不返回空对象
		if targetChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "当前节点不存在，无法生成摘要",
			})
			return
		}

		// 调用 AI 生成摘要
		abstract, err := targetChain.GenerateAbstract(body.CustomUrl, body.CustomKey)

		// 拦截任何报错（包括超时、401、网络异常、没对话记录等）
		if err != nil {
			if err.Error() == "AUTH_ERROR" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "生成摘要失败: " + err.Error(),
			})
			return
		}

		// 剥离前后空格后如果还是空的，直接拒绝落盘并报错
		if strings.TrimSpace(abstract) == "" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "大模型返回了空的摘要，已被系统拦截",
			})
			return
		}

		// 只有确保内容不为空，才落盘并返回成功响应
		mu.Lock()
		rootChain.SaveChainToFile("chain_history.json")
		mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{
			"abstract": abstract,
		})
	})

	http.HandleFunc("/api/switch-side-to-main", func(w http.ResponseWriter, r *http.Request) {
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
			// forkNode.DialogSide = nil

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

	http.HandleFunc("/api/archive-main", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if currentChain == nil {
			http.Error(w, "未初始化", http.StatusBadRequest)
			return
		}

		// 1. 校验：必须是主线才能点这个“归档”
		if currentChain.BranchColor != YellowNode {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "归档功能仅限主线使用"})
			return
		}

		// 2. 校验：必须先有摘要才能归档
		if currentChain.DialogAbstract == "" {
			w.WriteHeader(http.StatusPreconditionFailed) // 412
			return
		}

		// 3. 【核心操作】调用你的封装函数开启新主线节点
		// 这会自动创建 newNode，并将摘要作为 system 消息塞进去
		newNode := currentChain.AppendMainBranchNode()

		// 4. 更新指针并保存
		currentChain = newNode
		rootChain.SaveChainToFile("chain_history.json")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "主线已翻页，开启新章节",
		})
	})

	fmt.Println("服务已启动，请在浏览器中打开: http://127.0.0.1:8099")
	http.ListenAndServe(":8099", nil)
}
