// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
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
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var (
	mu               sync.Mutex
	OpenRouterKey    string
	rootChain        *chatChain
	currentChain     *chatChain
	queryEngine      *query.Engine // Global query engine
	currentParserCmd *exec.Cmd
	parserMu         sync.Mutex
)

const (
	tagOK    = "\033[1;32m[OK]\033[0m"
	tagError = "\033[1;31m[ERROR]\033[0m"
	tagWarn  = "\033[1;33m[WARN]\033[0m"
	tagInfo  = "\033[1;36m[INFO]\033[0m"
	tagMatch = "\033[1;35m[MATCH]\033[0m"
	tagDone  = "\033[1;32m[DONE]\033[0m"
)

var isNoWebUI bool

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

func killParserCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = kill.Run()
	} else {
		_ = cmd.Process.Kill()
	}
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

func hideConsoleWindow() {
	if runtime.GOOS != "windows" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")

	procGetConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	procShowWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd != 0 {
		procShowWindow.Call(hwnd, 0)
	}
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
	if isNoWebUI {
		return
	}

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

				parserMu.Lock()
				if currentParserCmd != nil && currentParserCmd.Process != nil {
					killParserCmd(currentParserCmd)
				}
				parserMu.Unlock()

				os.Exit(0)
			}
		})
	}
}

func main() {

	// port configs
	defaultPort := os.Getenv("PORT")
	if defaultPort == "" {
		defaultPort = "8099"
	}

	portStr := flag.String("port", defaultPort, "Server listen port (set to 0 for random port)")
	noWebUI := flag.Bool("noWebUI", false, "Run without opening Web UI in browser and disable auto-exit")
	flag.Parse()
	isNoWebUI = *noWebUI

	if !*noWebUI {
		hideConsoleWindow()
	}

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

	// ========================  routers  ===============================================
	registerRoutes()
	// ==================================================================================

	listener, err := net.Listen("tcp", "127.0.0.1:"+*portStr)
	if err != nil {
		logPrintf("%s Failed to bind port %s: %v\n", tagError, *portStr, err)
		return
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	logPrintf("%s Server started, please open in browser: \033[1;34m%s\033[0m\n", tagInfo, serverURL)

	if !*noWebUI {
		// default mode
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
	} else {
		logPrintf("%s Running in --noWebUI mode (browser launch & auto-exit disabled)\n", tagInfo)
	}

	if err := http.Serve(listener, nil); err != nil {
		logPrintf("%s Server error: %v\n", tagError, err)
	}
}
