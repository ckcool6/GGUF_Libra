package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// enum
type NodeColor int

const (
	YellowNode NodeColor = iota // 0: Main branch
	GreenNode                   // 1: Side branch
)

type chatChain struct {
	// data
	DialogContent  *chatlist
	DialogAbstract string

	// structure
	DialogMain      *chatChain
	DialogSide      *chatChain
	HistoryArchives []*chatChain `json:"history_archives,omitempty"`

	BranchColor  NodeColor
	IsForkedNode bool

	ActiveThoughtChain string `json:"ActiveThoughtChain,omitempty"`
}

// init
func (chain *chatChain) initChatChain() {
	if chain == nil {
		return
	}

	// Initialize chat list container for current node
	chain.DialogContent = &chatlist{
		ChatHistory:  make([]Message, 0),
		SendHistory:  make([]Message, 0),
		UserMsgIndex: -1,
	}

	chain.DialogAbstract = ""
	chain.DialogMain = nil
	chain.DialogSide = nil
	chain.BranchColor = YellowNode
	chain.IsForkedNode = false
}

// NewChatChain creates and returns an initialized chatChain node pointer
func NewChatChain() *chatChain {
	chain := &chatChain{}
	chain.initChatChain()
	return chain
}

// GenerateAbstract extracts summary
func (chain *chatChain) GenerateAbstract(customUrl, customKey string) (string, error) {
	if chain == nil || chain.DialogContent == nil || len(chain.DialogContent.ChatHistory) == 0 {
		return "", fmt.Errorf("no chat history available")
	}

	// Construct prompt specifically for summarization
	var promptMessages []LlamaMessage
	promptMessages = append(promptMessages, LlamaMessage{
		Role:    "system",
		Content: "You are a concise text summarization assistant. Summarize the key points of the dialogue within 100-200 words. You must generate the summary in the same primary language used in the conversation history (do not default to English).",
	})

	for _, m := range chain.DialogContent.ChatHistory {
		content := m.Content

		if m.Role == "system" {
			// Allow system messages containing context markers to enter summarization source material
			// This ensures the AI takes previous chapter summaries into account when summarizing current chapter
			if !strings.Contains(content, "Previous Context") && !strings.Contains(content, "Context Summary") {
				continue
			}
		}

		if m.Image != "" {
			content = "[Image Message] " + content
		}

		promptMessages = append(promptMessages, LlamaMessage{
			Role:    m.Role,
			Content: content,
		})
	}

	promptMessages = append(promptMessages, LlamaMessage{
		Role:    "user",
		Content: "Please generate a brief context summary for the conversation above.Summarize the key points of the dialogue within 100-200 words. You must generate the summary in the same primary language used in the conversation history (do not default to English).",
	})

	payload := map[string]interface{}{
		"messages": promptMessages,
		"stream":   false,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	apiURL := "http://127.0.0.1:8021/v1/chat/completions"
	if customUrl != "" {
		apiURL = customUrl
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("❌ Failed to create summary request:", err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// Handle API key forwarding
	if customKey != "" {
		if strings.HasPrefix(customKey, "Bearer ") {
			req.Header.Set("Authorization", customKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+customKey)
		}
	}

	resp, err := httpTimeoutClient.Do(req)
	if err != nil {
		fmt.Println("❌ Summary API request failed:", err)
		return "", err
	}
	defer resp.Body.Close()

	// Handle 401 Unauthorized
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("AUTH_ERROR") // Return specific error sentinel for handler identification
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Failed to generate summary, status code %d: %s\n", resp.StatusCode, string(bodyBytes))
		return "", fmt.Errorf("API response error: %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("❌ Failed to parse summary response:", err)
		return "", err
	}

	if len(result.Choices) > 0 {
		// Trim leading and trailing whitespace
		abstract := strings.TrimSpace(result.Choices[0].Message.Content)
		if abstract == "" {
			return "", fmt.Errorf("AI returned empty content")
		}

		chain.DialogAbstract = abstract
		return abstract, nil
	}

	return "", fmt.Errorf("API returned empty choices list")
}

// AppendMainBranchNode appends a new main branch child node to the current node
func (chain *chatChain) AppendMainBranchNode() *chatChain {
	if chain == nil {
		return nil
	}

	newNode := NewChatChain()
	chain.DialogMain = newNode
	newNode.BranchColor = chain.BranchColor
	newNode.DialogAbstract = chain.DialogAbstract

	if chain.DialogAbstract != "" {
		newNode.DialogContent.ChatHistory = append(newNode.DialogContent.ChatHistory, Message{
			Role:    "system",
			Content: "[Previous Context / Context Summary]:\n" + chain.DialogAbstract,
		})
	}

	return newNode
}

// AppendSideBranchNode creates and initializes a side branch node
func (chain *chatChain) AppendSideBranchNode(withSummary bool) *chatChain {
	if chain == nil {
		return nil
	}

	newNode := NewChatChain()
	chain.DialogSide = newNode
	newNode.IsForkedNode = true
	newNode.BranchColor = GreenNode

	// Inject previous context summary only if requested and available
	if withSummary && chain.DialogAbstract != "" {
		newNode.DialogContent.ChatHistory = append(newNode.DialogContent.ChatHistory, Message{
			Role:    "system",
			Content: "[Previous Context / Context Summary]:\n" + chain.DialogAbstract,
		})
	}

	return newNode
}

// BackToLastForkedNode finds and returns a pointer to the nearest preceding fork node from the current node
func (root *chatChain) BackToLastForkedNode(currentNode *chatChain) *chatChain {
	if root == nil || currentNode == nil || root == currentNode {
		return nil
	}

	type pathNode struct {
		node *chatChain
		path []*chatChain
	}

	stack := []pathNode{{node: root, path: []*chatChain{root}}}

	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if curr.node == currentNode {
			for i := len(curr.path) - 2; i >= 0; i-- {
				if curr.path[i].IsForkedNode || curr.path[i].DialogSide != nil {
					return curr.path[i]
				}
			}
			return nil
		}

		if curr.node.DialogSide != nil {
			newPath := append(append([]*chatChain{}, curr.path...), curr.node.DialogSide)
			stack = append(stack, pathNode{node: curr.node.DialogSide, path: newPath})
		}
		if curr.node.DialogMain != nil {
			newPath := append(append([]*chatChain{}, curr.path...), curr.node.DialogMain)
			stack = append(stack, pathNode{node: curr.node.DialogMain, path: newPath})
		}
	}

	return nil
}

// BackToMainForkedNode ignores all micro-forks within side branches and traces directly back to the nearest fork point on the main branch
func (root *chatChain) BackToMainForkedNode(currentNode *chatChain) *chatChain {
	if root == nil || currentNode == nil || root == currentNode {
		return nil
	}

	var path []*chatChain
	var findPath func(node *chatChain) bool

	findPath = func(node *chatChain) bool {
		if node == nil {
			return false
		}
		path = append(path, node)
		if node == currentNode {
			return true
		}
		if findPath(node.DialogMain) || findPath(node.DialogSide) {
			return true
		}
		path = path[:len(path)-1]
		return false
	}

	if !findPath(root) {
		return nil
	}

	// Search in reverse from parent nodes; must satisfy both: is YellowNode (main branch) and possesses fork characteristics
	for i := len(path) - 2; i >= 0; i-- {
		n := path[i]
		if n.BranchColor == YellowNode && (n.IsForkedNode || n.DialogSide != nil) {
			return n
		}
	}

	return nil
}

// Merge executes merge logic: find fork point -> locate main branch tail -> graft summary node
func (root *chatChain) Merge(currentNode *chatChain) (*chatChain, error) {
	if root == nil || currentNode == nil {
		return nil, fmt.Errorf("node cannot be nil")
	}

	// Retrieve side branch summary (requires generate-abstract to be triggered beforehand)
	summary := currentNode.DialogAbstract
	if summary == "" {
		return nil, fmt.Errorf("side branch summary has not been generated yet, please generate summary before merging")
	}

	// Trace back to identify which main branch fork point this side branch originated from
	forkNode := root.BackToMainForkedNode(currentNode)
	if forkNode == nil {
		return nil, fmt.Errorf("failed to locate fork origin of this side branch")
	}

	// Locate the deepest tail of the current main canonical branch
	// Ensure the merged result is grafted onto the bottom of the main branch
	mainTail := root
	for mainTail.DialogMain != nil {
		mainTail = mainTail.DialogMain
	}

	// Create brand new merge node
	// This node encapsulates the achievements of the side branch and extends the main branch
	mergedNode := NewChatChain()
	mergedNode.BranchColor = YellowNode // Return to main branch
	mergedNode.IsForkedNode = false     // Convergence node
	mergedNode.DialogAbstract = summary // Transfer side branch summary to main dialog

	// Assemble merge message
	mergeMsg := Message{
		Role:    "assistant",
		Content: "[Context] We previously explored the following topic in depth; continuing conversation on this basis:\n\n" + summary,
	}
	mergedNode.DialogContent.ChatHistory = append(mergedNode.DialogContent.ChatHistory, mergeMsg)

	// Execute merge physically (atomic operation)
	// a. Point main branch tail to the new node
	mainTail.DialogMain = mergedNode

	if forkNode.DialogSide != nil {
		forkNode.HistoryArchives = append(forkNode.HistoryArchives, forkNode.DialogSide)
	}

	// b. Prune side branch: disconnect side branch from fork point
	// The side branch is logically detached, leaving only the summary within the main branch
	forkNode.DialogSide = nil

	// Return newly created main branch node to allow main.go to update currentChain
	return mergedNode, nil
}

// EditAbstract manually overwrites and modifies summary
func (chain *chatChain) EditAbstract(newAbstract string) {
	if chain == nil {
		return
	}
	chain.DialogAbstract = newAbstract
}

// SaveChainToFile serializes and persists chain to JSON file
func (chain *chatChain) SaveChainToFile(filePath string) error {
	if chain == nil {
		return nil
	}

	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		fmt.Println("❌ Failed to serialize chatChain:", err)
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// LoadChainFromFile restores chain from JSON file
func LoadChainFromFile(filePath string) (*chatChain, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var root chatChain
	if err := json.Unmarshal(data, &root); err != nil {
		fmt.Println("❌ Failed to deserialize chatChain:", err)
		return nil, err
	}

	return &root, nil
}
