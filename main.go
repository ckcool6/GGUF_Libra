// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"gguf-libra/query"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	mu            sync.Mutex
	OpenRouterKey string
	rootChain     *chatChain
	currentChain  *chatChain
	queryEngine   *query.Engine // Global query engine
)

const (
	tagOK    = "\033[1;32m[OK]\033[0m"
	tagError = "\033[1;31m[ERROR]\033[0m"
	tagWarn  = "\033[1;33m[WARN]\033[0m"
	tagInfo  = "\033[1;36m[INFO]\033[0m"
	tagMatch = "\033[1;35m[MATCH]\033[0m"
	tagDone  = "\033[1;32m[DONE]\033[0m"
)

// ==================== SYSTEM LOG CAPTURE & BROADCAST ====================

type LogHub struct {
	mu         sync.Mutex
	clients    map[*websocket.Conn]bool
	history    [][]byte
	maxHistory int
}

var logHub = &LogHub{
	clients:    make(map[*websocket.Conn]bool),
	history:    make([][]byte, 0, 300),
	maxHistory: 300,
}

func (h *LogHub) register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true

	for _, chunk := range h.history {
		_ = conn.WriteMessage(websocket.BinaryMessage, chunk)
	}
}

func (h *LogHub) unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	_ = conn.Close()
}

func (h *LogHub) broadcast(chunk []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	saved := make([]byte, len(chunk))
	copy(saved, chunk)
	h.history = append(h.history, saved)
	if len(h.history) > h.maxHistory {
		h.history = h.history[len(h.history)-h.maxHistory:]
	}

	for conn := range h.clients {
		_ = conn.WriteMessage(websocket.BinaryMessage, chunk)
	}
}

func initGlobalLogCapture() {
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Println("Failed to create log pipe:", err)
		return
	}

	origStdout := os.Stdout
	os.Stdout = w
	os.Stderr = w

	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])

				if origStdout != nil {
					_, _ = origStdout.Write(chunk)
				}

				text := strings.ReplaceAll(string(chunk), "\r\n", "\n")
				text = strings.ReplaceAll(text, "\n", "\r\n")
				logHub.broadcast([]byte(text))
			}
			if err != nil {
				break
			}
		}
	}()
}

// log info
var appStartTime = time.Now()

func uptime() string {
	d := time.Since(appStartTime)
	totalSec := int(d.Seconds())
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	ms := d.Milliseconds() % 1000

	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func logPrintf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)

	for strings.HasPrefix(msg, "\n") {
		fmt.Print("\n")
		msg = msg[1:]
	}

	if strings.HasPrefix(msg, "\r") {
		fmt.Printf("\r%s %s", uptime(), msg[1:])
		return
	}

	fmt.Printf("%s %s", uptime(), msg)
}

func logPrintln(a ...any) {
	fmt.Print(uptime(), " ")
	fmt.Println(a...)
}

var (
	// Client for persistent connections / streaming chat requests
	httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Client for standard timeout requests (e.g., generating summaries, fetching props)
	httpTimeoutClient = &http.Client{
		Timeout: 600 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		},
	}
)

// Calculate the lateral branch depth of the current node relative to the backbone
func getSideBranchDepth(root, current *chatChain) int {
	if root == nil || current == nil || current.BranchColor == YellowNode {
		return 0
	}

	var path []*chatChain
	var findPath func(node *chatChain) bool
	findPath = func(node *chatChain) bool {
		if node == nil {
			return false
		}
		path = append(path, node)
		if node == current {
			return true
		}
		if findPath(node.DialogMain) || findPath(node.DialogSide) {
			return true
		}
		path = path[:len(path)-1]
		return false
	}

	if !findPath(root) {
		return 1
	}

	// side node count
	depth := 0
	for _, n := range path {
		if n.BranchColor == GreenNode {
			depth++
		}
	}
	if depth == 0 {
		return 1
	}
	return depth
}

func getCurrentBranchName(chain *chatChain) string {
	if chain == nil || chain.BranchColor == YellowNode {
		return "main"
	}
	// eg. side#1, side#2
	depth := getSideBranchDepth(rootChain, chain)
	return fmt.Sprintf("side#%d", depth)
}

// reloadQueryEngine reload data.bin
func reloadQueryEngine() {
	newEngine, err := query.NewEngine("data.bin")
	if err != nil {
		logPrintf("%s Failed to reload data.bin on new chat: %v\n", tagError, err)
		return
	}

	mu.Lock()
	queryEngine = newEngine
	mu.Unlock()

	logPrintf("%s data.bin reloaded successfully on new chat! Total dialogue trees: %d\n", tagOK, len(newEngine.Records))
}

// open parser helps
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func sendWsError(conn *websocket.Conn, format string, a ...any) {
	msg := fmt.Appendf(nil, "\r\n\x1b[31m"+format+"\x1b[0m\r\n", a...)
	_ = conn.WriteMessage(websocket.TextMessage, msg)
}

func resolveParserCmd() (*exec.Cmd, error) {
	binName := "parser"
	if runtime.GOOS == "windows" {
		binName = "parser.exe"
	}

	candidates := []string{
		filepath.Join("parser_py", binName),
		binName,
	}

	for _, path := range candidates {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			if runtime.GOOS != "windows" {
				_ = os.Chmod(path, 0755)
			}
			return exec.Command(path), nil
		}
	}

	pyScriptCandidates := []string{
		filepath.Join("parser_py", "main.py"),
		"main.py",
	}

	var targetPyScript string
	for _, p := range pyScriptCandidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			targetPyScript = p
			break
		}
	}

	if targetPyScript != "" {
		pyExecs := []string{"python3", "python"}
		if runtime.GOOS == "windows" {
			pyExecs = []string{"python", "python3"}
		}

		for _, py := range pyExecs {
			if _, err := exec.LookPath(py); err == nil {
				return exec.Command(py, targetPyScript), nil
			}
		}
		return nil, fmt.Errorf("found %s but no python interpreter found in PATH", targetPyScript)
	}

	return nil, fmt.Errorf("neither binary (%s) nor parser.py found", binName)
}

func initParserTerminalHandler() {
	http.HandleFunc("/ws/parser", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logPrintf("%s WebSocket upgrade error: %v\n", tagError, err)
			return
		}
		defer conn.Close()

		cmd, err := resolveParserCmd()
		if err != nil {
			sendWsError(conn, "[Error] %v", err)
			return
		}

		stdin, err := cmd.StdinPipe()
		if err != nil {
			sendWsError(conn, "[Error] Stdin pipe failed: %v", err)
			return
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			sendWsError(conn, "[Error] Stdout pipe failed: %v", err)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			sendWsError(conn, "[Error] Stderr pipe failed: %v", err)
			return
		}

		logPrintf("%s [Parser] Spawning process: %s (OS: %s)\n", tagInfo, cmd.String(), runtime.GOOS)

		if err := cmd.Start(); err != nil {
			sendWsError(conn, "[Error] Failed to start process: %v", err)
			return
		}

		defer func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				logPrintf("%s [Parser] Process terminated (PID: %d)\n", tagInfo, cmd.Process.Pid)
			}
		}()

		done := make(chan struct{})

		// Stdout -> WebSocket
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				}
				if err != nil {
					break
				}
			}
			close(done)
		}()

		// Stderr -> WebSocket
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				}
				if err != nil {
					break
				}
			}
		}()

		// WebSocket -> Stdin
		go func() {
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				_, _ = stdin.Write(msg)
			}
		}()

		<-done
		_ = cmd.Wait()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[33m[Process exited]\x1b[0m\r\n"))
	})
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux 等
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ==================== APP LIFELINE (AUTO-EXIT) ====================
var (
	liveClientCount int
	liveMu          sync.Mutex
	exitTimer       *time.Timer
)

func cancelExitCountdown() {
	liveMu.Lock()
	defer liveMu.Unlock()
	liveClientCount++
	if exitTimer != nil {
		exitTimer.Stop()
		exitTimer = nil
	}
}

func startExitCountdown() {
	liveMu.Lock()
	defer liveMu.Unlock()

	if liveClientCount <= 0 {
		if exitTimer != nil {
			exitTimer.Stop()
		}
		exitTimer = time.AfterFunc(5*time.Second, func() {
			liveMu.Lock()
			defer liveMu.Unlock()
			if liveClientCount <= 0 {
				logPrintf("%s All UI tabs closed, auto-exiting program...\n", tagInfo)
				os.Exit(0)
			}
		})
	}
}

func main() {

	initGlobalLogCapture()

	var err error
	query.LogFunc = logPrintf

	// Initialize and load data.bin (assign to global queryEngine)
	queryEngine, err = query.NewEngine("data.bin")
	if err != nil {
		logPrintf("%s Failed to load data.bin: %v\n", tagError, err)
	} else {
		logPrintf("%s data.bin loaded successfully! Total dialogue trees: %d\n", tagOK, len(queryEngine.Records))
	}

	config_init()
	initRAG()

	// parser terminal
	initParserTerminalHandler()

	// log terminal
	http.HandleFunc("/ws/logs", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logPrintf("%s WebSocket upgrade error: %v\n", tagError, err)
			return
		}
		logHub.register(conn)
		defer logHub.unregister(conn)

		_ = conn.WriteMessage(websocket.TextMessage, []byte("\x1b[32m[Connected to Go Backend Log Stream]\x1b[0m\r\n"))

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})

	http.HandleFunc("/ws/live", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		cancelExitCountdown()

		defer func() {
			liveMu.Lock()
			liveClientCount--
			liveMu.Unlock()
			startExitCountdown()
		}()

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	})

	// Host static assets from the dist directory
	http.Handle("/", http.FileServer(http.Dir("dist")))

	// Chat API endpoint
	http.HandleFunc("/api/chat", chatHandler)

	// History API endpoint
	http.HandleFunc("/api/history", apiHistoryHandler)

	// New chat API endpoint
	http.HandleFunc("/api/new-chat", apiNewChatHandler)

	// Retrieve context props
	http.HandleFunc("/api/llama-props", apiLlamaPropsHandler)

	// Switch prompt endpoints
	http.HandleFunc("/api/prompts", apiGetPromptsHandler)
	http.HandleFunc("/api/switch-prompt", apiSwitchPromptHandler)

	// Delete last message round endpoint
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

	// Fork a side branch handler
	http.HandleFunc("/api/fork-side", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		// Validation: A summary must exist prior to forking, otherwise reject
		if currentChain == nil || currentChain.DialogAbstract == "" {
			logPrintf("%s [Fork] Action rejected (412): a summary is required on current node prior to forking\n", tagWarn)
			w.WriteHeader(http.StatusPreconditionFailed) // 412
			json.NewEncoder(w).Encode(map[string]string{
				"error": "A summary and archive are required on the main branch before forking.",
			})
			return
		}

		// Inherit summary by default
		withSummary := true
		if r.Method == http.MethodPost {
			var req ForkSideRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				withSummary = req.WithSummary
			}
		}

		// Validation：Disallow isolated fork on side branch (GreenNode); must inherit summary.
		if currentChain != nil && currentChain.BranchColor == GreenNode && !withSummary {
			logPrintf("%s [Fork] Detected side branch; isolated fork is prohibited, forcing withSummary=true\n", tagWarn)
			withSummary = true
		}

		if currentChain != nil {
			parentAbstract := currentChain.DialogAbstract
			parentPrompt := currentChain.PromptID
			// Create side branch child node and point currentChain to the new branch
			currentChain = currentChain.AppendSideBranchNode(withSummary)
			currentChain.PromptID = parentPrompt

			rootChain.SaveChainToFile("chain_history.json")

			preview := strings.ReplaceAll(safeTruncate(parentAbstract, 60), "\n", " ")
			logPrintf("%s [Fork] Side branch successfully created & activated (Green Node)!\n", tagOK)
			logPrintf("  - Inherited Summary : %v (\"%s...\")\n", withSummary, preview)
			logPrintf("  - Vector Isolation  : Vector DB cleared to isolate side exploration\n")
			logPrintf("  - Active Pointer    : Moved to new green side branch (chain_history.json saved)\n")
		}

		w.WriteHeader(http.StatusOK)
	})

	// Merge side branch back to main branch: insert side branch summary as a new node at the end of the main branch
	http.HandleFunc("/api/merge", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if rootChain == nil || currentChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Dialogue session not initialized"})
			return
		}

		// Execute merge logic
		// rootChain is needed for DFS path searching
		// currentChain is the leaf node of the side branch carrying the summary
		newNode, err := rootChain.Merge(currentChain)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Crucial step: point the user's active pointer back to the newly merged main line node
		currentChain = newNode

		if currentChain != nil && currentChain.PromptID != "" {
			setActivePrompt(currentChain.PromptID)
		}
		// Persist state to file
		rootChain.SaveChainToFile("chain_history.json")
		// log
		preview := strings.ReplaceAll(safeTruncate(newNode.DialogAbstract, 60), "\n", " ")
		logPrintf("%s [Merge] Side branch successfully merged & grafted into main branch tail!\n", tagOK)
		logPrintf("  - Merged Abstract: \"%s...\"\n", preview)
		logPrintf("  - Topology Action: Side branch archived to HistoryArchives and pruned from fork node\n")
		logPrintf("  - Active Pointer  : Pointed to new convergence main node (chain_history.json saved)\n")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Side branch successfully merged into the end of main branch",
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

		// If the node does not exist, return 400 Bad Request
		if targetChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Current node does not exist, unable to generate summary",
			})
			return
		}

		// Call AI to generate summary
		abstract, err := targetChain.GenerateAbstract(body.CustomUrl, body.CustomKey)

		// Catch any error (timeout, 401, network failure, empty history, etc.)
		if err != nil {
			if err.Error() == "AUTH_ERROR" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Failed to generate summary: " + err.Error(),
			})
			return
		}

		// Reject and return error if summary is empty after trimming
		if strings.TrimSpace(abstract) == "" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "LLM returned an empty summary, rejected by system",
			})
			return
		}

		// Persist state to file and respond with success only when content is not empty
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

		forkNode := rootChain.BackToMainForkedNode(currentChain)

		if forkNode != nil && forkNode.DialogSide != nil {
			markChainDiscarded(forkNode.DialogSide)
		} else if currentChain.BranchColor == GreenNode {
			markChainDiscarded(currentChain)
		}

		// If fork point not found, it is already on the main branch or tree structure is abnormal
		if forkNode == nil {
			// Fallback: point currentChain directly to the tail of the main branch
			currentChain = rootChain
			for currentChain.DialogMain != nil {
				currentChain = currentChain.DialogMain
			}
		} else {
			// Return to canonical branch: locate the current tail of the main branch
			mainTail := rootChain
			for mainTail.DialogMain != nil {
				mainTail = mainTail.DialogMain
			}
			currentChain = mainTail
		}

		if currentChain != nil && currentChain.PromptID != "" {
			setActivePrompt(currentChain.PromptID)
		}

		rootChain.SaveChainToFile("chain_history.json")

		// log
		tailPreview := "(empty / new chapter)"
		if currentChain.DialogAbstract != "" {
			tailPreview = strings.ReplaceAll(safeTruncate(currentChain.DialogAbstract, 60), "\n", " ") + "..."
		}
		logPrintf("%s [Switch] Successfully switched from side branch to main branch tail!\n", tagOK)
		logPrintf("  - Active Main Tail Node: \"%s\"\n", tailPreview)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Side branch discarded, returned to main branch tail",
		})
	})

	// API endpoint for editing summary
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
			// Persist immediately to prevent data loss
			rootChain.SaveChainToFile("chain_history.json")

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}

		http.Error(w, "Node not found", http.StatusNotFound)
	})

	http.HandleFunc("/api/archive-main", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if currentChain == nil {
			http.Error(w, "Not initialized", http.StatusBadRequest)
			return
		}

		// Validation: archive action is strictly limited to the main branch
		if currentChain.BranchColor != YellowNode {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "Archiving is only permitted on the main branch"})
			return
		}

		// Validation: a summary must exist prior to archiving
		if currentChain.DialogAbstract == "" {
			w.WriteHeader(http.StatusPreconditionFailed) // 412
			return
		}

		// Core operation: spawn a new main branch node
		// This automatically creates newNode and injects the summary as a system message
		oldAbstract := currentChain.DialogAbstract
		newNode := currentChain.AppendMainBranchNode()

		// Update pointer and persist state
		currentChain = newNode
		rootChain.SaveChainToFile("chain_history.json")

		preview := strings.ReplaceAll(safeTruncate(oldAbstract, 60), "\n", " ")
		logPrintf("%s [Archive] Main branch archived successfully! Started new chapter (chain_history.json updated)\n", tagOK)
		logPrintf("  - Inherited Summary: \"%s...\"\n", preview)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Main branch archived, started a new chapter",
		})
	})

	http.HandleFunc("/api/upload-doc", uploadDocHandler)
	http.HandleFunc("/api/edit-latest-ai", editLatestAiHandler)

	// port configs
	defaultPort := os.Getenv("PORT")
	if defaultPort == "" {
		defaultPort = "8099"
	}

	portStr := flag.String("port", defaultPort, "Server listen port (set to 0 for random port)")
	flag.Parse()

	listener, err := net.Listen("tcp", "127.0.0.1:"+*portStr)
	if err != nil {
		logPrintf("%s Failed to bind port %s: %v\n", tagError, *portStr, err)
		return
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	logPrintf("%s Server started, please open in browser: \033[1;34m%s\033[0m\n", tagInfo, serverURL)

	exitTimer = time.AfterFunc(15*time.Second, func() {
		liveMu.Lock()
		defer liveMu.Unlock()
		if liveClientCount <= 0 {
			logPrintf("%s Browser failed to connect in time, exiting...\n", tagWarn)
			os.Exit(0)
		}
	})

	go func() {
		time.Sleep(150 * time.Millisecond)
		openBrowser(serverURL)
	}()

	if err := http.Serve(listener, nil); err != nil {
		logPrintf("%s Server error: %v\n", tagError, err)
	}
}
