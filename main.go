// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"gguf-libra/query"
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

func getCurrentBranchName(chain *chatChain) string {
	if chain == nil {
		return "main"
	}
	if chain.BranchColor == GreenNode || chain.IsForkedNode {
		return "side"
	}
	return "main"
}

func main() {

	var err error

	// Initialize and load data.bin (assign to global queryEngine)
	queryEngine, err = query.NewEngine("data.bin")
	if err != nil {
		logPrintf("%s Failed to load data.bin: %v\n", tagError, err)
	} else {
		logPrintf("%s data.bin loaded successfully! Total dialogue trees: %d\n", tagOK, len(queryEngine.Records))
	}

	config_init()
	initRAG()

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
		ClearVectorDB() // Prevent side branch from being affected by the vector knowledge base

		// Inherit summary by default
		withSummary := true
		if r.Method == http.MethodPost {
			var req ForkSideRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				withSummary = req.WithSummary
			}
		}

		mu.Lock()
		if currentChain != nil {
			// Create side branch child node and point currentChain to the new branch
			currentChain = currentChain.AppendSideBranchNode(withSummary)
			rootChain.SaveChainToFile("chain_history.json")
		}
		mu.Unlock()
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

		// Persist state to file
		rootChain.SaveChainToFile("chain_history.json")

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
		ClearVectorDB() // Clear vector database so model only relies on the summary
	})

	http.HandleFunc("/api/switch-side-to-main", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if rootChain == nil || currentChain == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Locate the fork point where this side branch diverged from the main branch
		forkNode := rootChain.BackToMainForkedNode(currentChain)

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

		// Persist state to file
		rootChain.SaveChainToFile("chain_history.json")

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
		newNode := currentChain.AppendMainBranchNode()

		// Update pointer and persist state
		currentChain = newNode
		rootChain.SaveChainToFile("chain_history.json")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Main branch archived, started a new chapter",
		})
	})

	http.HandleFunc("/api/upload-doc", uploadDocHandler)

	logPrintf("%s Server started, please open in browser: \033[1;34mhttp://127.0.0.1:8099\033[0m\n", tagInfo)
	http.ListenAndServe(":8099", nil)
}
