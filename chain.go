package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	DialogMain *chatChain
	DialogSide *chatChain

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
func (chain *chatChain) GenerateAbstract(customUrl, customKey string) string {
	if chain == nil || chain.DialogContent == nil || len(chain.DialogContent.ChatHistory) == 0 {
		return ""
	}

	// 1. 读取历史记录
	history := make([]Message, len(chain.DialogContent.ChatHistory))
	copy(history, chain.DialogContent.ChatHistory)

	// 2. 构造专门用于总结的 Prompt
	promptMessages := []Message{
		{
			Role:    "system",
			Content: "你是一个精炼的文本总结助手。请用简明扼要的语言总结以下对话的核心要点与关键上下文，字数控制在100-200字以内，不要有多余的客套话。",
		},
	}
	promptMessages = append(promptMessages, history...)
	promptMessages = append(promptMessages, Message{
		Role:    "user",
		Content: "请为以上的对话生成一份简短的上下文摘要总结。",
	})

	payload := map[string]interface{}{
		"messages": promptMessages,
		"stream":   false,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("❌ 总结请求失败:", err)
		return ""
	}

	apiURL := "http://127.0.0.1:8021/v1/chat/completions"
	if customUrl != "" {
		apiURL = customUrl
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("❌ 创建总结请求失败:", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	if customKey != "" {
		req.Header.Set("Authorization", "Bearer "+customKey)
	}

	resp, err := httpTimeoutClient.Do(req)
	if err != nil {
		fmt.Println("❌ 请求总结 API 失败:", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ 生成总结失败，响应码 %d: %s\n", resp.StatusCode, string(bodyBytes))
		return ""
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
		io.Copy(io.Discard, resp.Body)
		return ""
	}

	if len(result.Choices) > 0 {
		abstract := result.Choices[0].Message.Content
		chain.DialogAbstract = abstract
		return abstract
	}

	return ""
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

// RebaseAllSideToMain 将当前节点下的侧线分支完整合并（变基压平）到主线末尾，不丢失任何后续节点
func (chain *chatChain) RebaseAllSideToMain() {
	if chain == nil || chain.DialogSide == nil {
		return
	}

	sideHead := chain.DialogSide

	// 1. 遍历侧线，修饰节点属性并找到侧线的最深末尾
	sideTail := sideHead
	for {
		sideTail.BranchColor = YellowNode
		sideTail.IsForkedNode = false

		if sideTail.DialogMain != nil {
			sideTail = sideTail.DialogMain
		} else if sideTail.DialogSide != nil {
			sideTail = sideTail.DialogSide
		} else {
			break
		}
	}

	// 2. 找到主线最深的尾巴 (Main Tail)
	mainTail := chain
	for mainTail.DialogMain != nil {
		mainTail = mainTail.DialogMain
	}

	// 3. 把侧线整体接在主线最末尾，并清空原本的 DialogSide
	mainTail.DialogMain = sideHead
	chain.DialogSide = nil
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
