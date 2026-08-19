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
	YellowNode NodeColor = iota // 0: 主线
	GreenNode                   // 1: 侧线
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
}

// init
func (chain *chatChain) initChatChain() {
	if chain == nil {
		return
	}

	// 初始化当前节点的对话列表容器
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

// NewChatChain 创建并返回一个初始化好的 chatChain 节点指针
func NewChatChain() *chatChain {
	chain := &chatChain{}
	chain.initChatChain()
	return chain
}

// GenerateAbstract 提取摘要
func (chain *chatChain) GenerateAbstract(customUrl, customKey string) (string, error) {
	if chain == nil || chain.DialogContent == nil || len(chain.DialogContent.ChatHistory) == 0 {
		return "", fmt.Errorf("没有对话记录")
	}

	// 构造专门用于总结的 Prompt
	var promptMessages []LlamaMessage
	promptMessages = append(promptMessages, LlamaMessage{
		Role:    "system",
		Content: "你是一个精炼的文本总结助手。请总结对话核心要点，字数控制在100-200字以内。",
	})

	for _, m := range chain.DialogContent.ChatHistory {
		if m.Role == "system" {
			continue
		}
		content := m.Content
		if m.Image != "" {
			content = "[图片消息] " + content // 仅保留占位符
		}
		promptMessages = append(promptMessages, LlamaMessage{
			Role:    m.Role,
			Content: content,
		})
	}

	promptMessages = append(promptMessages, LlamaMessage{
		Role:    "user",
		Content: "请为以上的对话生成一份简短的上下文摘要总结。",
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
		fmt.Println("❌ 创建总结请求失败:", err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// 处理 Key 的转发
	if customKey != "" {
		if strings.HasPrefix(customKey, "Bearer ") {
			req.Header.Set("Authorization", customKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+customKey)
		}
	}

	resp, err := httpTimeoutClient.Do(req)
	if err != nil {
		fmt.Println("❌ 请求总结 API 失败:", err)
		return "", err
	}
	defer resp.Body.Close()

	// --- 处理 401 错误 ---
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("AUTH_ERROR") // 返回特定错误，让 Handler 能够识别
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ 生成总结失败，响应码 %d: %s\n", resp.StatusCode, string(bodyBytes))
		return "", fmt.Errorf("API 响应错误: %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("❌ 解析总结响应失败:", err)
		return "", err
	}

	if len(result.Choices) > 0 {
		abstract := result.Choices[0].Message.Content
		chain.DialogAbstract = abstract
		return abstract, nil // 成功返回摘要和 nil 错误
	}

	return "", fmt.Errorf("API 返回了空的选择列表")
}

// AppendMainBranchNode 为当前节点追加一个新的主线子节点
func (chain *chatChain) AppendMainBranchNode() *chatChain {
	if chain == nil {
		return nil
	}

	newNode := NewChatChain()
	chain.DialogMain = newNode
	newNode.BranchColor = chain.BranchColor

	if chain.DialogAbstract != "" {
		newNode.DialogContent.ChatHistory = append(newNode.DialogContent.ChatHistory, Message{
			Role:    "system",
			Content: "【前情提要/历史上下文总结】：\n" + chain.DialogAbstract,
		})
	}

	return newNode
}

// AppendSideBranchNode 为当前节点追加一个新的侧线分支节点
func (chain *chatChain) AppendSideBranchNode() *chatChain {
	if chain == nil {
		return nil
	}

	newNode := NewChatChain()
	chain.DialogSide = newNode
	newNode.IsForkedNode = true
	newNode.BranchColor = GreenNode

	if chain.DialogAbstract != "" {
		newNode.DialogContent.ChatHistory = append(newNode.DialogContent.ChatHistory, Message{
			Role:    "system",
			Content: "【前情提要/历史上下文总结】：\n" + chain.DialogAbstract,
		})
	}

	return newNode
}

// BackToLastForkedNode 查找并返回离当前节点最近的上一个分叉节点指针
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

// BackToMainForkedNode 忽略侧线内部的所有微型分叉，直接回退到主线上最近的那个分叉点
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

	// 从父节点开始倒序查找，必须同时满足：是 YellowNode（主线）且拥有分叉特征
	for i := len(path) - 2; i >= 0; i-- {
		n := path[i]
		if n.BranchColor == YellowNode && (n.IsForkedNode || n.DialogSide != nil) {
			return n
		}
	}

	return nil
}

// Merge 执行合并逻辑：找到分叉点 -> 找到主线末尾 -> 嫁接摘要节点
func (root *chatChain) Merge(currentNode *chatChain) (*chatChain, error) {
	if root == nil || currentNode == nil {
		return nil, fmt.Errorf("节点不能为空")
	}

	// 1. 获取侧线摘要 (前提是已经在前端触发了 generate-abstract)
	summary := currentNode.DialogAbstract
	if summary == "" {
		return nil, fmt.Errorf("侧线尚未生成摘要，请先生成摘要再合并")
	}

	// 2. 回溯寻找该侧线是从哪个主线分叉点出来的
	forkNode := root.BackToMainForkedNode(currentNode)
	if forkNode == nil {
		return nil, fmt.Errorf("无法定位该侧线的分叉源头")
	}

	// 3. 寻找当前主线（正史）的最深末尾
	// 我们要保证合并成果是接在主线最下方的
	mainTail := root
	for mainTail.DialogMain != nil {
		mainTail = mainTail.DialogMain
	}

	// 4. 创建全新的合并节点
	// 这个节点将承载侧线的成果，并作为主线的延伸
	mergedNode := NewChatChain()
	mergedNode.BranchColor = YellowNode // 回归主线
	mergedNode.IsForkedNode = false     // 它是汇聚点

	// 5. 组装合并消息
	mergeMsg := Message{
		Role:    "assistant",
		Content: "【背景】刚才我们深入探讨了以下内容,以此为基础继续对话:\n\n" + summary,
	}
	mergedNode.DialogContent.ChatHistory = append(mergedNode.DialogContent.ChatHistory, mergeMsg)

	// 6. 物理执行合并（原子操作）
	// a. 将主线末尾指向新节点
	mainTail.DialogMain = mergedNode

	if forkNode.DialogSide != nil {
		forkNode.HistoryArchives = append(forkNode.HistoryArchives, forkNode.DialogSide)
	}

	// b. 收割侧线：断开分叉点与侧线的连接
	// 这样整棵侧线在逻辑上就“消失”了，只有摘要留在了主线里
	forkNode.DialogSide = nil

	// 返回这个新产生的主线节点，以便 main.go 更新 currentChain
	return mergedNode, nil
}

// EditAbstract 手动覆盖修改摘要
func (chain *chatChain) EditAbstract(newAbstract string) {
	if chain == nil {
		return
	}
	chain.DialogAbstract = newAbstract
}

// SaveChainToFile 序列化保存到 JSON 文件
func (chain *chatChain) SaveChainToFile(filePath string) error {
	if chain == nil {
		return nil
	}

	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		fmt.Println("❌ 序列化 chatChain 失败:", err)
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// LoadChainFromFile 从 JSON 文件还原
func LoadChainFromFile(filePath string) (*chatChain, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var root chatChain
	if err := json.Unmarshal(data, &root); err != nil {
		fmt.Println("❌ 反序列化 chatChain 失败:", err)
		return nil, err
	}

	return &root, nil
}
