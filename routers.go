// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"syscall"

	"github.com/gorilla/websocket"
)

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

		if runtime.GOOS == "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
			v := reflect.ValueOf(cmd.SysProcAttr).Elem()

			if f := v.FieldByName("HideWindow"); f.IsValid() && f.CanSet() {
				f.SetBool(true)
			}
			if f := v.FieldByName("CreationFlags"); f.IsValid() && f.CanSet() {
				f.SetUint(0x08000000) // CREATE_NO_WINDOW
			}
		}

		if err := cmd.Start(); err != nil {
			sendWsError(conn, "[Error] Failed to start process: %v", err)
			return
		}

		parserMu.Lock()
		currentParserCmd = cmd
		parserMu.Unlock()

		defer func() {
			parserMu.Lock()
			currentParserCmd = nil
			parserMu.Unlock()

			if cmd.Process != nil {
				killParserCmd(cmd)
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

			defer func() {
				_ = stdin.Close()
				if cmd.Process != nil {
					killParserCmd(cmd)
				}
			}()

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

func registerRoutes() {
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
}
