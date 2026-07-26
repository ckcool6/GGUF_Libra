package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// enum
type nodeColor int

const (
	YellowNode nodeColor = iota // 0
	GreenNode
)

type chatChain struct {
	// data
	dialogContent  *chatlist
	dialogAbstract string

	// structure
	dialogMain *chatChain
	dialogSide *chatChain

	branchColor  nodeColor
	isForkedNode bool
}

// init
func (chain *chatChain) initChatChain() {
	if chain == nil {
		return
	}

	// 初始化当前节点的对话列表容器
	chain.dialogContent = &chatlist{
		chatHistory:  make([]Message, 0),
		sendHistory:  make([]Message, 0),
		userMsgIndex: -1,
	}

	//  初始化节点的摘要信息
	chain.dialogAbstract = ""

	//  重置主线和侧线指针
	chain.dialogMain = nil
	chain.dialogSide = nil

	//  设置节点属性
	chain.branchColor = YellowNode // 默认设为主线颜色
	chain.isForkedNode = false
}

// NewChatChain 创建并返回一个初始化好的 chatChain 节点指针
func NewChatChain() *chatChain {
	chain := &chatChain{}
	chain.initChatChain()
	return chain
}

// eval context
func (chain *chatChain) GenerateAbstract(customUrl, customKey string) string {
	if chain == nil || chain.dialogContent == nil || len(chain.dialogContent.chatHistory) == 0 {
		return ""
	}

	mu.Lock()
	// 提取当前节点保存的所有对话内容
	history := chain.dialogContent.chatHistory
	mu.Unlock()

	// 构造专门用于总结的 Prompt
	promptMessages := []Message{
		{
			Role:    "system",
			Content: "你是一个精炼的文本总结助手。请用简明扼要的语言总结以下对话的核心要点与关键上下文，字数控制在100-200字以内，不要有多余的客套话。",
		},
	}

	// 将当前 Block 的历史记录追加进来
	promptMessages = append(promptMessages, history...)
	promptMessages = append(promptMessages, Message{
		Role:    "user",
		Content: "请为以上的对话生成一份简短的上下文摘要总结。",
	})

	// 构造请求 Payload（总结不使用流式）
	payload := map[string]interface{}{
		"messages": promptMessages,
		"stream":   false,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("❌ 序列化总结请求失败:", err)
		return ""
	}

	// 解析与拼接 API 请求地址[cite: 3]
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

	// 如果设置了 customKey，加上 Bearer 鉴权[cite: 3]
	if customKey != "" {
		req.Header.Set("Authorization", "Bearer "+customKey)
	}

	// 发送请求（设置 30 秒超时）
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

	// 解析响应
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("❌ 解析总结响应失败:", err)
		return ""
	}

	if len(result.Choices) > 0 {
		abstract := result.Choices[0].Message.Content

		// 7. 保存摘要到当前节点[cite: 4]
		mu.Lock()
		chain.dialogAbstract = abstract
		mu.Unlock()

		return abstract
	}

	return ""
}

// AppendMainBranchNode 为当前节点追加一个新的主线子节点，并返回新创建的节点指针
func (chain *chatChain) AppendMainBranchNode() *chatChain {
	if chain == nil {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	// 创建并初始化一个新的 ChatChain 节点
	newNode := NewChatChain()

	// 将主线指针指向新节点
	chain.dialogMain = newNode

	// 新节点继承当前节点的 branchColor，保持主线颜色一致
	newNode.branchColor = chain.branchColor

	// 如果当前节点已经生成了摘要，将摘要传递给新节点的起始背景
	// 这样下一个 Context Block 在向 llamacpp 发送请求时，就能带上上一个 Block 的总结
	if chain.dialogAbstract != "" {
		newNode.dialogContent.chatHistory = append(newNode.dialogContent.chatHistory, Message{
			Role:    "system",
			Content: "【前情提要/历史上下文总结】：\n" + chain.dialogAbstract,
		})
	}

	return newNode
}

// AppendSideBranchNode 为当前节点追加一个新的侧线（分支）节点，并返回新创建的节点指针
func (chain *chatChain) AppendSideBranchNode() *chatChain {
	if chain == nil {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	// 创建并初始化一个新的 ChatChain 节点
	newNode := NewChatChain()

	// 将侧线指针指向新节点，并将新节点标记为 Fork 节点
	chain.dialogSide = newNode
	newNode.isForkedNode = true

	// 将侧线节点的颜色区分开（设为绿色的 GreenNode）
	newNode.branchColor = GreenNode

	// 新节点继承当前节点的摘要信息（如果有的话），保证上下文不断层
	if chain.dialogAbstract != "" {
		newNode.dialogContent.chatHistory = append(newNode.dialogContent.chatHistory, Message{
			Role:    "system",
			Content: "【前情提要/历史上下文总结】：\n" + chain.dialogAbstract,
		})
	}

	return newNode
}

// sidebranch status
// BackToLastForkedNode 查找并返回离当前节点最近的上一个分叉节点指针
func (root *chatChain) BackToLastForkedNode(currentNode *chatChain) *chatChain {
	if root == nil || currentNode == nil || root == currentNode {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	var lastForked *chatChain

	// 使用 DFS 路径追踪找到通往 currentNode 的路径
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

		// 优先走主线
		if findPath(node.dialogMain) {
			return true
		}

		// 再走侧线
		if findPath(node.dialogSide) {
			return true
		}

		// 没找到则回溯
		path = path[:len(path)-1]
		return false
	}

	// 1. 寻找从根节点到当前节点的路径
	if findPath(root) {
		// 2. 从路径倒数第二个节点往回找，找到第一个 isForkedNode 为 true 的节点
		for i := len(path) - 2; i >= 0; i-- {
			if path[i].isForkedNode || path[i].dialogSide != nil {
				lastForked = path[i]
				break
			}
		}
	}

	return lastForked
}

// RebaseAllSideToMain 将当前节点下的侧线分支全部合并（压平）到主线末尾
func (chain *chatChain) RebaseAllSideToMain() {
	if chain == nil || chain.dialogSide == nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// 拿到侧线分支的头节点
	sideHead := chain.dialogSide

	// 遍历侧线，把所有侧线节点的属性修正为主线属性
	sideTail := sideHead
	for sideTail != nil {
		sideTail.branchColor = YellowNode
		sideTail.isForkedNode = false

		// 如果侧线节点自身还有子主线，优先顺着主线摸到侧线的最深末尾
		if sideTail.dialogMain != nil {
			sideTail = sideTail.dialogMain
		} else if sideTail.dialogSide != nil {
			// 如果侧线节点上又套了侧线，顺手接上
			sideTail = sideTail.dialogSide
		} else {
			break
		}
	}

	// 寻找当前主线的末尾节点 (Main Tail)
	mainTail := chain
	for mainTail.dialogMain != nil {
		mainTail = mainTail.dialogMain
	}

	// 将侧线链条整体挂载到主线末尾，并清空原节点的 dialogSide 指针
	mainTail.dialogMain = sideHead
	chain.dialogSide = nil
}

// EditAbstract 允许用户或前端手动覆盖修改当前节点的摘要内容
func (chain *chatChain) EditAbstract(newAbstract string) {
	if chain == nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// 直接覆盖当前节点的摘要
	chain.dialogAbstract = newAbstract
}

// SaveChainToFile 将整棵树/链序列化保存到 JSON 文件
func (chain *chatChain) SaveChainToFile(filePath string) error {
	if chain == nil {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		fmt.Println("❌ 序列化 chatChain 失败:", err)
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// LoadChainFromFile 从 JSON 文件还原整个 chatChain 结构
func LoadChainFromFile(filePath string) (*chatChain, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	defer mu.Unlock()

	var root chatChain
	if err := json.Unmarshal(data, &root); err != nil {
		fmt.Println("❌ 反序列化 chatChain 失败:", err)
		return nil, err
	}

	return &root, nil
}

//
/* // BackToLastForkedNode 迭代（非递归）版本的广度/深度优先遍历
func (root *chatChain) BackToLastForkedNodeIterative(currentNode *chatChain) *chatChain {
	if root == nil || currentNode == nil || root == currentNode {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	// 用 slice 模拟手动的栈/队列，避免任何函数递归调用栈开销
	type pathNode struct {
		node *chatChain
		path []*chatChain
	}

	stack := []pathNode{{node: root, path: []*chatChain{root}}}

	for len(stack) > 0 {
		// 弹出栈顶
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if curr.node == currentNode {
			// 找到路径，从倒数第二个节点往回找分叉点
			for i := len(curr.path) - 2; i >= 0; i-- {
				if curr.path[i].isForkedNode || curr.path[i].dialogSide != nil {
					return curr.path[i]
				}
			}
			return nil
		}

		// 将侧线和主线入栈
		if curr.node.dialogSide != nil {
			newPath := append(append([]*chatChain{}, curr.path...), curr.node.dialogSide)
			stack = append(stack, pathNode{node: curr.node.dialogSide, path: newPath})
		}
		if curr.node.dialogMain != nil {
			newPath := append(append([]*chatChain{}, curr.path...), curr.node.dialogMain)
			stack = append(stack, pathNode{node: curr.node.dialogMain, path: newPath})
		}
	}

	return nil
} */
