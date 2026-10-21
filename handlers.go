// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gguf-libra/query"
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

	logPrintln("====================== RAG Vector Debug Info ============================")
	logPrintf("User query: %s\n", body.Message)
	if vErr != nil {
		logPrintf("%s Retrieval error: %v\n", tagWarn, vErr)
	} else if len(matchedDocs) == 0 {
		logPrintf("%s \033[38;2;230;130;50mMissed\033[0m No relevant snippets matched\n", tagInfo)
	} else {
		logPrintf("%s Retrieval successful, \033[1;34mmatched\033[0m %d snippet(s)\n", tagOK, len(matchedDocs))
		logPrintf("%s Preview of first matched snippet:\n%s\n", tagInfo, matchedDocs[0])
	}
	logPrintln("=========================================================================")

	// =========================================================================
	// Core addition: Topological chain-of-thought retrieval (data.bin) + detailed logs
	// =========================================================================
	var thoughtChainSnippet string

	if queryEngine != nil {
		logPrintln("====================== Chain DB Debug Info ==============================")

		// Query active top 15% memory pool (pass 0 to let dynamic retention take over)
		qRes, qErr := queryEngine.QuerySemantic(ctx, body.Message, 0, llamaEmbeddingFunc)

		if qErr != nil {
			// ==========================================
			// Case 1: No new tree matched, check if current branch has anchored memory
			// ==========================================
			if localChain.ActiveThoughtChain != "" {
				// Current query didn't match a new tree; invalidate/clear previous thought chain to avoid stale context pollution
				thoughtChainSnippet = ""
				localChain.ActiveThoughtChain = ""
			}
			logPrintf("%s [Chain of Thought] %v; retaining natural context\n", tagInfo, qErr)
		} else {
			// ==========================================
			// Case 2: New tree matched! Log details and anchor to current branch
			// ==========================================
			logPrintf("========= %s Matched & Anchored New Thought Tree (data.bin) ========\n", tagMatch)
			logPrintf("%s Matched chain activated (LogicT: \033[1;35m%.4f\033[0m)\n", tagOK, qRes.Record.LogicT)
			logPrintf("  - Matched Tree UUID : %d\n", qRes.Record.UUID)
			logPrintf("  - Tree Date/Time    : %s\n", qRes.Record.Date)
			logPrintf("  - Matched Keyword   : [%s]\n", qRes.Record.Keyword)
			logPrintf("  - Derivation Chain  : %v (%d reasoning steps)\n", qRes.NodePath, len(qRes.EdgePath))

			// Assemble chain-of-thought prompt snippet
			const maxStepLen = 60

			var rawNodes []string
			for i, step := range qRes.EdgePath {
				pText := strings.TrimSpace(step.ParentText)
				cText := strings.TrimSpace(step.ChildText)

				if pText == cText || cText == "" {
					logPrintf("\033[93m[WARN]\033[0m Skipped step (Edge %d: Node %d -> Node %d), reason: pText==cText (%v), cText is empty (%v)\n",
						i+1, step.ParentID, step.ChildID, pText == cText, cText == "")
					continue
				}

				if len(rawNodes) == 0 {
					rawNodes = append(rawNodes, strings.ReplaceAll(pText, "\n", " "))
				}
				rawNodes = append(rawNodes, strings.ReplaceAll(cText, "\n", " "))
			}

			var nodeSnippets []string
			for idx, text := range rawNodes {
				t := truncateRunes(text, maxStepLen)
				if len([]rune(text)) > maxStepLen && !strings.HasSuffix(t, "...") {
					t += "..."
				}
				nodeSnippets = append(nodeSnippets, fmt.Sprintf("%d. [%s]", idx+1, t))
			}

			stepsContent := strings.Join(nodeSnippets, " -> ")

			logPrintf("%s Pure reasoning steps:\n  %s\n", tagInfo, stepsContent)

			thoughtChainSnippet = fmt.Sprintf(
				"[VERIFIED HISTORICAL PROGRESSION]:\n"+
					"The timeline below records how the current topic originally unfolded with the user.\n"+
					"INSTRUCTION: Recount this full progression naturally within your response, explaining the context from the beginning up to the current topic. Do not use bullet points, numbered lists, subheadings, or step labels:\n"+
					"  %s\n",
				stepsContent,
			)

			// Core action: persist assembled thought chain to current branch for multi-turn conversations
			localChain.ActiveThoughtChain = thoughtChainSnippet

			// Log the first and last steps as a preview
			if len(qRes.EdgePath) > 0 {
				firstStep := qRes.EdgePath[0]
				lastStep := qRes.EdgePath[len(qRes.EdgePath)-1]
				logPrintf("  - Initial Step : [%d] %s... -> [%d] %s...\n",
					firstStep.ParentID, truncateRunes(firstStep.ParentText, 20),
					firstStep.ChildID, truncateRunes(firstStep.ChildText, 20))
				logPrintf("  - Terminal Step: [%d] -> [%d] %s...\n",
					lastStep.ParentID, lastStep.ChildID, truncateRunes(lastStep.ChildText, 30))
			}
		}

		logPrintln("=========================================================================")
	}

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
		promptPrefix.WriteString("[Reference Context]:\n")
		promptPrefix.WriteString(strings.Join(matchedDocs, "\n---\n"))
		promptPrefix.WriteString("\n\n")
	}

	if promptPrefix.Len() > 0 {
		promptPrefix.WriteString("Respond to the user's message strictly based on the context and reasoning history provided above:\n\n")
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
		logPrintf("%s Failed to connect to llama.cpp service: %v\n", tagError, err)
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
			branchType := getCurrentBranchName(node)
			nodeMsgs := []Message{}
			for _, msg := range node.DialogContent.ChatHistory {
				if msg.Role != "system" {
					msg.Branch = branchType
					nodeMsgs = append(nodeMsgs, msg)
				}
			}

			if len(nodeMsgs) > 0 {
				lastIdx := len(nodeMsgs) - 1
				nodeMsgs[lastIdx].Abstract = node.DialogAbstract

				if len(node.HistoryArchives) > 0 {
					for _, archChain := range node.HistoryArchives {
						archMsgs := extractAllMessages(archChain)
						nodeMsgs[lastIdx].Archives = append(nodeMsgs[lastIdx].Archives, archMsgs)
					}
				}
			} else if len(node.HistoryArchives) > 0 && len(history) > 0 {
				prevIdx := len(history) - 1
				for _, archChain := range node.HistoryArchives {
					archMsgs := extractAllMessages(archChain)
					history[prevIdx].Archives = append(history[prevIdx].Archives, archMsgs)
				}
			}

			history = append(history, nodeMsgs...)
		}

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

// Helper: Discard and close response body to reuse connections
func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// Helper: Dispatch an erase action for a specific slot ID
func eraseSingleSlot(baseURL string, slotID int) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/slots/%d?action=erase", baseURL, slotID), nil)
	if err != nil {
		return
	}
	resp, err := httpTimeoutClient.Do(req)
	if err != nil {
		return
	}
	drainAndClose(resp)
}

// Dynamically query and erase llama.cpp KV cache across all available slots
func eraseAllLlamaSlots(customURL string) {
	baseURL := "http://127.0.0.1:8021"
	if customURL != "" {
		if parsedURL, err := url.Parse(customURL); err == nil && parsedURL.Host != "" {
			baseURL = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
		}
	}

	go func() {
		// Retrieve all active slot states
		resp, err := httpTimeoutClient.Get(baseURL + "/slots")
		if err != nil || resp.StatusCode != http.StatusOK {
			// Fallback: attempt to erase default slot 0 if /slots endpoint is unreachable
			eraseSingleSlot(baseURL, 0)
			return
		}
		defer drainAndClose(resp)

		var slots []struct {
			ID int `json:"id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil || len(slots) == 0 {
			// Fallback: attempt to erase default slot 0 if decode fails or empty
			eraseSingleSlot(baseURL, 0)
			return
		}

		// Iterate through and erase each active slot
		for _, s := range slots {
			eraseSingleSlot(baseURL, s.ID)
		}
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
	eraseAllLlamaSlots(customURL)

	// Clean up chromem-go in-memory collections and disk .gob files
	if err := ClearVectorDB(); err != nil {
		logPrintf("%s Failed to clean up vector database/gob files: %v\n", tagWarn, err)
	} else {
		logPrintf("%s Successfully reset chromem-go vector database and cleared persisted .gob files!\n", tagOK)
	}

	if newEngine, err := query.NewEngine("data.bin"); err != nil {
		logPrintf("%s Failed to reload data.bin on new chat: %v\n", tagError, err)
	} else {
		mu.Lock()
		queryEngine = newEngine
		mu.Unlock()
		logPrintf("%s data.bin reloaded successfully on new chat! Total dialogue trees: %d\n", tagOK, len(newEngine.Records))
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
			logPrintf("\n%s Client disconnected; stopped receiving stream.\n", tagWarn)
			respBody.Close()
			break Loop
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				logPrintf("%s Non-EOF error encountered while reading stream: %v\n", tagWarn, err)
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
				logPrintf("\n> %s\n", tagDone)
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
		logPrintf("%s Failed to parse client request: %v\n", tagError, err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return err
	}
	logPrintf("%s User Input: %s\n", tagInfo, body.Message)
	return nil
}

func apiGetPromptsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	if err := loadSystemPrompt("system_prompt.yaml"); err != nil {
		logPrintf("%s loading YAML error: %v\n", tagWarn, err)
	}

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
	if err := loadSystemPrompt("system_prompt.yaml"); err != nil {
		logPrintf("%s loading YAML error: %v\n", tagWarn, err)
	}
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

	logPrintf("%s Extracted %d syntax chunks from \"%s\" and indexed into vector DB\n", tagOK, len(chunks), header.Filename)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Successfully chunked and indexed %d code/text snippet(s)", len(chunks)),
	})
}

func editLatestAiHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req editLatestAiReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if currentChain == nil || currentChain.DialogContent == nil {
		http.Error(w, "No active chain found", http.StatusBadRequest)
		return
	}

	chatHist := currentChain.DialogContent.ChatHistory
	found := false

	for i := len(chatHist) - 1; i >= 0; i-- {
		if chatHist[i].Role == "assistant" {
			currentChain.DialogContent.ChatHistory[i].Content = req.Content
			found = true
			break
		}
	}

	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "The latest conversation has been checkpointed (archived). Checkpointed responses cannot be edited.",
		})
		return
	}

	sendHist := currentChain.DialogContent.SendHistory
	for i := len(sendHist) - 1; i >= 0; i-- {
		if sendHist[i].Role == "assistant" {
			currentChain.DialogContent.SendHistory[i].Content = req.Content
			break
		}
	}

	if rootChain != nil {
		rootChain.SaveChainToFile("chain_history.json")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// helper
func truncateRunes(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen])
}
