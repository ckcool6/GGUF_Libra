package main

import (
	"fmt"
	"github.com/joho/godotenv"
	"net/http"
	"os"
	"sync"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
)

func main() {

	godotenv.Load() // 自动读取 .env 文件并加载到环境变量
	OpenRouterKey = os.Getenv("OPENROUTER_KEY")

	loadHistoryFromFile() // 启动即加载
	config_init()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// 聊天接口
	http.HandleFunc("/api/chat", chatHandler)

	http.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "manifest.json")
	})

	http.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "sw.js")
	})

	http.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "icon.png")
	})

	// 修改 /api/history 路由
	http.HandleFunc("/api/history", apiHistoryHandler)

	// 修改 /api/new-chat 路由
	http.HandleFunc("/api/new-chat", apiNewChatHandler)

	// 修改 /api/delete-last 路由
	http.HandleFunc("/api/delete-last", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock() // 加锁
		if len(chatHistory) >= 2 {
			chatHistory = chatHistory[:len(chatHistory)-2]
			saveHistoryToFile()
		}
		mu.Unlock() // 解锁
		w.WriteHeader(http.StatusOK)
	})

	// 在 main.go 中添加这一行
	http.HandleFunc("/chat.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "chat.js")
	})

	fmt.Println("服务已启动: http://0.0.0.0:8024")
	http.ListenAndServe(":8024", nil)
}
