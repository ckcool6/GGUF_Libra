package main

import (
	"fmt"
	"net/http"
	"sync"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
	globalId      *chatlist = &chatlist{chatHistory: []Message{}}
)

func main() {

	loadHistoryFromFile() // 启动即加载
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

	// 修改 /api/delete-last 路由
	http.HandleFunc("/api/delete-last", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		if len(globalId.chatHistory) >= 2 {
			globalId.chatHistory = globalId.chatHistory[:len(globalId.chatHistory)-2]
			saveHistoryToFile()
		}

		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	fmt.Println("服务已启动，请在浏览器中打开: http://127.0.0.1:8099")
	http.ListenAndServe(":8099", nil)
}
