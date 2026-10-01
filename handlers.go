package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Dynamically retrieve the current model's maximum n_ctx context limit
func getLlamaMaxCtx(customURL string) int {
	apiURL := "http://127.0.0.1:8021/props"
	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			apiURL = fmt.Sprintf("%s://%s/props", parsedURL.Scheme, parsedURL.Host)
		}
	}

	// Initiate request using globally shared httpTimeoutClient
	resp, err := httpTimeoutClient.Get(apiURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return 512 // Fallback to llama.cpp's default minimum of 512 if unavailable
	}
	defer resp.Body.Close()

	var propsData struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}

	// Parse response JSON
	if err := json.NewDecoder(resp.Body).Decode(&propsData); err == nil && propsData.DefaultGenerationSettings.NCtx > 0 {
		return propsData.DefaultGenerationSettings.NCtx
	}

	return 512
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	var body reqBody
	if err := parse_input(r, w, &body); err != nil {
		return
	}

	// Acquire local reference and immediately release global mutex
	mu.Lock()
	localChain := currentChain
	mu.Unlock()

	if localChain == nil {
		http.Error(w, "No active chain", http.StatusBadRequest)
		return
	}

	// =========================================================================
	// Vector database retrieval (fine-grained document facts)
	// =========================================================================
	embURL := getEmbeddingsURL(body.CustomEmbeddingUrl)
	ctx := context.WithValue(r.Context(), embeddingURLKey, embURL)

	if auth := r.Header.Get("Authorization"); auth != "" {
		ctx = context.WithValue(ctx, "auth_header", auth)
	} else if body.CustomKey != "" {
		ctx = context.WithValue(ctx, "auth_header", "Bearer "+body.CustomKey)
	}

	matchedDocs, vErr := queryVectorDB(ctx, body.Message, 3)

	fmt.Println("================ RAG Vector Debug Info ================")
	fmt.Printf("1. User query: %s\n", body.Message)
	if vErr != nil {
		fmt.Printf("2. [warning] Retrieval missed: %v\n", vErr)
	} else {
		fmt.Printf("2. [OK] Retrieval successful, matched %d snippet(s)\n", len(matchedDocs))
		if len(matchedDocs) > 0 {
			fmt.Printf("3. 📌 Preview of first matched snippet:\n%s\n", matchedDocs[0])
		}
	}
	fmt.Println("=======================================================")

	// =========================================================================
	// Core addition: Topological chain-of-thought retrieval (data.bin) + detailed logs
	// =========================================================================
	var thoughtChainSnippet string

	if queryEngine != nil {
		// Query top 5 dialogue trees
		qRes, qErr := queryEngine.Query(r.Context(), 5, body.Message)

		if qErr != nil {
			// ==========================================
			// Case 1: No new tree matched, check if current branch has anchored memory
			// ==========================================
			if localChain.ActiveThoughtChain != "" {
				// Reusing previous thought map
				thoughtChainSnippet = localChain.ActiveThoughtChain
				fmt.Println("ℹ️  [Chain of Thought] No new tree triggered; successfully reusing anchored historical thought map!")
			} else {
				fmt.Println("ℹ️  [Chain of Thought] No external tree triggered; retaining existing context")
			}
		} else {
			// ==========================================
			// Case 2: New tree matched! Log details and anchor to current branch
			// ==========================================
			fmt.Println("================== 🎯 Matched & Anchored New Thought Tree (data.bin) ==================")
			fmt.Printf("  - Matched Tree UUID : %d\n", qRes.Record.UUID)
			fmt.Printf("  - Tree Logic T      : %.4f\n", qRes.Record.LogicT)
			fmt.Printf("  - Matched Keyword   : [%s]\n", qRes.Record.Keyword)
			fmt.Printf("  - Derivation Chain  : %v (%d reasoning steps)\n", qRes.NodePath, len(qRes.EdgePath))

			// Assemble chain-of-thought prompt snippet
			var sb strings.Builder
			sb.WriteString("[Relevant historical reasoning context (for evolutionary logic reference only)]:\n")
			for i, step := range qRes.EdgePath {
				sb.WriteString(fmt.Sprintf("  - Step %d: Based on [%s] -> Derived [%s]\n", i+1, step.ParentText, step.ChildText))
			}
			sb.WriteString("\n")
			thoughtChainSnippet = sb.String()

			// Core action: persist assembled thought chain to current branch for multi-turn conversations
			localChain.ActiveThoughtChain = thoughtChainSnippet

			// Log the first and last steps as a preview
			if len(qRes.EdgePath) > 0 {
				firstStep := qRes.EdgePath[0]
				lastStep := qRes.EdgePath[len(qRes.EdgePath)-1]
				fmt.Printf("  - Initial Step : [%d] %s... -> [%d] %s...\n",
					firstStep.ParentID, firstStep.ParentText[:min(20, len(firstStep.ParentText))],
					firstStep.ChildID, firstStep.ChildText[:min(20, len(firstStep.ChildText))])
				fmt.Printf("  - Terminal Step: [%d] -> [%d] %s...\n",
					lastStep.ParentID, lastStep.ChildID, lastStep.ChildText[:min(30, len(lastStep.ChildText))])
			}
			fmt.Println("================================================================")
		}
	}
	fmt.Println("==================================================")

	// =========================================================================
	// Hybrid context augmentation (chain-of-thought + reference docs + user query)
	// =========================================================================
	var promptPrefix strings.Builder

	// Causal chain-of-thought
	if thoughtChainSnippet != "" {
		promptPrefix.WriteString(thoughtChainSnippet)
	}

	// Vector DB reference documents
	if vErr == nil && len(matchedDocs) > 0 {
		promptPrefix.WriteString("[Reference Code / Documents]:\n")
		promptPrefix.WriteString(strings.Join(matchedDocs, "\n---\n"))
		promptPrefix.WriteString("\n\n")
	}

	if promptPrefix.Len() > 0 {
		promptPrefix.WriteString("If the reference content above is relevant to the user's question, incorporate it into your answer; otherwise, ignore it:\n\n")
	}

	// =========================================================================
	// Load history and dispatch request to local model (isolate UI display from model input)
	// =========================================================================
	// Load history with original message first (ensuring ChatHistory preserves clean user input)
	originalUserMsg := body.Message
	load_history(localChain, &body)

	// If prompt prefix exists, only augment the last message in SendHistory destined for the model
	if promptPrefix.Len() > 0 && len(localChain.DialogContent.SendHistory) > 0 {
		lastIdx := len(localChain.DialogContent.SendHistory) - 1
		enhancedPrompt := promptPrefix.String() + originalUserMsg

		// Augment only the payload dispatched to the model
		localChain.DialogContent.SendHistory[lastIdx].Content = enhancedPrompt
		body.Message = enhancedPrompt // Maintain compatibility with sendRequestToLlama
	}

	resp, err := sendRequestToLlama(r, &body, localChain.DialogContent.SendHistory)
	if err != nil {
		fmt.Println("❌ Failed to connect to llama.cpp service:", err)
		rollbackHistory(localChain)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Local API request failed"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		rollbackHistory(localChain)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Streaming transfer and post-processing
	aiFullContent, streamSuccess := forwardStreamData(w, r, resp.Body)

	if streamSuccess || aiFullContent != "" {
		localChain.DialogContent.ChatHistory = append(localChain.DialogContent.ChatHistory, Message{
			Role:    "assistant",
			Content: aiFullContent,
		})

		mu.Lock()
		rootChain.SaveChainToFile("chain_history.json")
		mu.Unlock()
	} else {
		rollbackHistory(localChain)
	}
}

func load_history(chain *chatChain, body *reqBody) {
	mu.Lock()
	defer mu.Unlock()

	if chain == nil || chain.DialogContent == nil {
		return
	}

	// Message entry must include Image
	chain.DialogContent.ChatHistory = append(chain.DialogContent.ChatHistory, Message{
		Role:    "user",
		Content: body.Message,
		Image:   body.Image, // Base64 image must be preserved in history
	})
	chain.DialogContent.UserMsgIndex = len(chain.DialogContent.ChatHistory) - 1

	// Calculate safeMaxTokens for the current node when sending to model
	maxCtx := getLlamaMaxCtx(body.CustomUrl)

	reserveTokens := 2048
	if maxCtx/5 < reserveTokens {
		reserveTokens = maxCtx / 5
	}

	safeMaxTokens := maxCtx - reserveTokens
	if safeMaxTokens < 100 {
		safeMaxTokens = 100
	}

	// Filter messages and populate SendHistory
	chain.DialogContent.SendHistory = filterMessagesByToken(chain.DialogContent.ChatHistory, safeMaxTokens)
}

func rollbackHistory(chain *chatChain) {
	mu.Lock()
	defer mu.Unlock()

	if chain == nil || chain.DialogContent == nil {
		return
	}

	idx := chain.DialogContent.UserMsgIndex
	if idx >= 0 && idx < len(chain.DialogContent.ChatHistory) {
		chain.DialogContent.ChatHistory = append(chain.DialogContent.ChatHistory[:idx], chain.DialogContent.ChatHistory[idx+1:]...)
	}
}

func apiHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("X-Current-Branch", getCurrentBranchName(currentChain))
	w.Header().Set("Access-Control-Expose-Headers", "X-Current-Branch")

	// Traverse rootChain directly from memory rather than re-reading the file
	// In-memory rootChain contains the most up-to-date state
	if rootChain == nil {
		json.NewEncoder(w).Encode([]Message{})
		return
	}

	history := []Message{}

	var collectMessages func(node *chatChain)
	collectMessages = func(node *chatChain) {
		if node == nil {
			return
		}

		if node.DialogContent != nil && len(node.DialogContent.ChatHistory) > 0 {
			// Extract non-system messages for this node
			nodeMsgs := []Message{}
			for _, msg := range node.DialogContent.ChatHistory {
				if msg.Role != "system" {
					nodeMsgs = append(nodeMsgs, msg)
				}
			}

			// Mount summary to node
			if len(nodeMsgs) > 0 {
				lastIdx := len(nodeMsgs) - 1
				// Assign node summary to the last visible message of this node
				nodeMsgs[lastIdx].Abstract = node.DialogAbstract

				// Process history archives
				if len(node.HistoryArchives) > 0 {
					for _, archChain := range node.HistoryArchives {
						archMsgs := extractAllMessages(archChain)
						nodeMsgs[lastIdx].Archives = append(nodeMsgs[lastIdx].Archives, archMsgs)
					}
				}
			}

			history = append(history, nodeMsgs...)
		}

		// Reverse DFS traversal
		collectMessages(node.DialogSide)
		collectMessages(node.DialogMain)
	}

	collectMessages(rootChain)
	json.NewEncoder(w).Encode(history)
}

// Helper: Flatten all messages from an archived branch for modal display
func extractAllMessages(node *chatChain) []Message {
	if node == nil {
		return nil
	}
	res := []Message{}
	if node.DialogContent != nil {
		for _, m := range node.DialogContent.ChatHistory {
			if m.Role != "system" {
				res = append(res, m)
			}
		}
	}
	res = append(res, extractAllMessages(node.DialogMain)...)
	res = append(res, extractAllMessages(node.DialogSide)...)
	return res
}

// Helper: Safely retrieve ChatHistory of the current node
func (chain *chatChain) dialogChainContentOrDefault() []Message {
	if chain == nil || chain.DialogContent == nil {
		return []Message{}
	}
	return chain.DialogContent.ChatHistory
}

// Dynamically erase llama.cpp KV cache for a specified slot
func eraseLlamaSlot(customURL string, slotID int) {
	apiURL := fmt.Sprintf("http://127.0.0.1:8021/slots/%d?action=erase", slotID)

	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			apiURL = fmt.Sprintf("%s://%s/slots/%d?action=erase", parsedURL.Scheme, parsedURL.Host, slotID)
		}
	}

	go func() {
		req, err := http.NewRequest("POST", apiURL, nil)
		if err != nil {
			return
		}

		// Dispatch asynchronous cleanup request using globally shared httpTimeoutClient
		resp, err := httpTimeoutClient.Do(req)
		if err != nil {
			return
		}

		// Ensure response body is drained and closed so the client can reuse TCP connections
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

func apiNewChatHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	// Initialize a clean tree root node in memory
	rootChain = NewChatChain()
	currentChain = rootChain

	// Overwrite chain_history.json with this new empty tree
	rootChain.SaveChainToFile("chain_history.json")
	mu.Unlock()

	// Dynamically erase llama.cpp slot 0 cache
	customURL := r.URL.Query().Get("custom_url")
	eraseLlamaSlot(customURL, 0)

	// Clean up chromem-go in-memory collections and disk .gob files
	if err := ClearVectorDB(); err != nil {
		fmt.Printf("⚠️ Failed to clean up vector database/gob files: %v\n", err)
	} else {
		fmt.Println("🧹 Successfully reset chromem-go vector database and cleared persisted .gob files!")
	}

	w.WriteHeader(http.StatusOK)
}

// Query llama.cpp /props endpoint to obtain actual context utilization
func apiLlamaPropsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	customURL := r.URL.Query().Get("custom_url")
	maxCtx := getLlamaMaxCtx(customURL)

	// Calculate token consumption of the active chat history in currentChain
	mu.Lock()
	currentTokens := 0
	if currentChain != nil && currentChain.DialogContent != nil {
		// Reserve headroom for model output tokens
		reserveTokens := 2048
		if maxCtx/5 < reserveTokens {
			reserveTokens = maxCtx / 5
		}
		safeMaxTokens := maxCtx - reserveTokens
		if safeMaxTokens < 100 {
			safeMaxTokens = 100
		}

		// Prune messages using safeMaxTokens to compute effective context tokens dispatched to model
		filteredMsgs := filterMessagesByToken(currentChain.DialogContent.ChatHistory, safeMaxTokens)
		for _, msg := range filteredMsgs {
			currentTokens += getMessageTokens(msg)
		}
	}
	mu.Unlock()

	responseData := map[string]interface{}{
		"default_generation_settings": map[string]interface{}{
			"n_ctx": maxCtx,
		},
		"slots": []map[string]interface{}{
			{
				"n_past": currentTokens,
			},
		},
	}

	json.NewEncoder(w).Encode(responseData)
}

// tool functions
func forwardStreamData(w http.ResponseWriter, r *http.Request, respBody io.ReadCloser) (string, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	reader := bufio.NewReader(respBody)

	var aiFullContent strings.Builder
	streamSuccess := false

Loop:
	for {
		select {
		case <-r.Context().Done():
			fmt.Println("\n🛑 Client disconnected; stopped receiving stream.")
			respBody.Close()
			break Loop
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Println("⚠️ Non-EOF error encountered while reading stream:", err)
			}
			break
		}

		if bytes.HasPrefix(line, []byte("data: ")) {
			if r.Context().Err() != nil {
				break
			}

			w.Write(line)
			w.Write([]byte("\n"))
			if flusher != nil {
				flusher.Flush()
			}

			data := bytes.TrimPrefix(line, []byte("data: "))
			data = bytes.TrimSpace(data)

			if bytes.Equal(data, []byte("[DONE]")) || bytes.Contains(data, []byte(`"done":true`)) {
				fmt.Println("\n> [DONE]")
				streamSuccess = true
				break
			}

			var streamResp struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &streamResp); err == nil && len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				aiFullContent.WriteString(content)
				fmt.Print(content)
			}
		}
	}

	io.Copy(io.Discard, respBody)
	return aiFullContent.String(), streamSuccess
}

func sendRequestToLlama(r *http.Request, body *reqBody, history []Message) (*http.Response, error) {
	formattedMessages := make([]LlamaMessage, 0, len(history))

	for _, m := range history {
		if m.Image != "" {
			// Image present: construct []LlamaContent slice
			contentArray := []LlamaContent{
				{Type: "text", Text: m.Content},
				{
					Type: "image_url",
					ImageURL: &LlamaImageDetail{
						URL: "data:image/jpeg;base64," + m.Image,
					},
				},
			}
			formattedMessages = append(formattedMessages, LlamaMessage{
				Role:    m.Role,
				Content: contentArray, // interface{} accepts slice
			})
		} else {
			// Text only: direct string assignment
			formattedMessages = append(formattedMessages, LlamaMessage{
				Role:    m.Role,
				Content: m.Content, // interface{} accepts string
			})
		}
	}

	payload := map[string]interface{}{
		"messages": formattedMessages,
		"stream":   true,
	}
	jsonData, _ := json.Marshal(payload)

	apiURL := "http://127.0.0.1:8021/v1/chat/completions"
	if body.CustomUrl != "" {
		apiURL = body.CustomUrl
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Prioritize custom API key from JSON body
	if body.CustomKey != "" {
		req.Header.Set("Authorization", "Bearer "+body.CustomKey)
	} else {
		// If absent in body, forward the Authorization header received from the client
		// Sent via client getHeaders(): "Bearer your_key"
		if auth := r.Header.Get("Authorization"); auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}

	return httpClient.Do(req)
}

func parse_input(r *http.Request, w http.ResponseWriter, body *reqBody) error {
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fmt.Println("❌ Failed to parse client request:", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return err
	}
	fmt.Println("> User Input:", body.Message)
	return nil
}

func apiGetPromptsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"active":  config.Active,
		"prompts": config.Prompts,
	})
}

func apiSwitchPromptHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	mu.Lock()
	success := setActivePrompt(body.ID)
	mu.Unlock()

	if !success {
		http.Error(w, "Prompt not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func uploadDocHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to parse uploaded file"})
		return
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(file)
	fileBytes := buf.Bytes()

	// AST code chunking: pass header.Filename as secondary parameter
	chunks := splitCodeWithTreeSitter(fileBytes, header.Filename)

	// Retrieve custom_url from request and inject into context
	authHeader := r.Header.Get("Authorization")
	customEmbURL := r.FormValue("custom_embedding_url")
	embURL := getEmbeddingsURL(customEmbURL)

	ctx := context.WithValue(r.Context(), embeddingURLKey, embURL)
	ctx = context.WithValue(ctx, "auth_header", authHeader)

	// Ingest into vector DB
	err = addChunksToVectorDB(ctx, chunks, header.Filename)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Vector database insertion failed: " + err.Error()})
		return
	}

	fmt.Printf("Successfully extracted %d syntax chunks from \"%s\" and indexed into vector DB\n", len(chunks), header.Filename)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Successfully chunked and indexed %d code/text snippet(s)", len(chunks)),
	})
}
