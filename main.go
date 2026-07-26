package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
	rootChain     *chatChain // 链表的根节点
	currentChain  *chatChain // 当前用户所在对话节点
)

func main() {
	var err error

	// 尝试从本地加载已保存的树状历史
	rootChain, err = LoadChainFromFile("chain_history.json")
	if err != nil || rootChain == nil {
		fmt.Println("未找到历史链文件，初始化新链...")
		rootChain = NewChatChain()
	} else {
		fmt.Println("成功加载历史链结构")
	}

	// 默认将 currentChain 指向主线最深处的末尾节点
	currentChain = rootChain
	for currentChain.DialogMain != nil {
		currentChain = currentChain.DialogMain
	}

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
	// main.go
	http.HandleFunc("/api/generate-abstract", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CustomUrl string `json:"custom_url"`
			CustomKey string `json:"custom_key"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		if currentChain != nil {
			// 同步等待摘要生成完成
			abstract := currentChain.GenerateAbstract(body.CustomUrl, body.CustomKey)

			mu.Lock()
			rootChain.SaveChainToFile("chain_history.json")
			mu.Unlock()

			// 将结果返回给前端
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
