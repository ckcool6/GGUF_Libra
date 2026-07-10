package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/joho/godotenv"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
	globalId      *chatlist = &chatlist{chatHistory: []Message{}}
)

func main() {

	godotenv.Load() // 自动读取 .env 文件并加载到环境变量
	OpenRouterKey = os.Getenv("OPENROUTER_KEY")

	loadHistoryFromFile() // 启动即加载
	config_init()

	// 核心全权接管：这一行代码会直接托管整个 dist 目录
	// 不管是普通的网页，还是 manifest.json、sw.js、icon.png，只要在 dist 目录下，它都能自动识别并发送
	http.Handle("/", http.FileServer(http.Dir("dist")))

	// 聊天接口
	http.HandleFunc("/api/chat", chatHandler)

	// 修改 /api/history 路由
	http.HandleFunc("/api/history", apiHistoryHandler)

	// 修改 /api/new-chat 路由
	http.HandleFunc("/api/new-chat", apiNewChatHandler)

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
