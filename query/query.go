package query

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

type Entry struct {
	StartID int      `msgpack:"start_id"`
	EndID   int      `msgpack:"end_id"`
	RawData []string `msgpack:"raw_data"`
}

type TreeMatrix struct {
	NodeCount  int     `msgpack:"node_count"`
	EntryCount int     `msgpack:"entry_count"`
	Entries    []Entry `msgpack:"entries"`
}

type Record struct {
	LogicT  float64    `msgpack:"logic_t"`
	UUID    int64      `msgpack:"uuid"`
	Date    string     `msgpack:"date"`
	Keyword string     `msgpack:"keyword"`
	Matrix  TreeMatrix `msgpack:"matrix"`
}

// PathStep 表示思考链路上的单步推理
type PathStep struct {
	ParentID   int    `json:"parent_id"`
	ChildID    int    `json:"child_id"`
	ParentText string `json:"parent_text"`
	ChildText  string `json:"child_text"`
}

// QueryResult 查询到的最终结果
type QueryResult struct {
	Record   Record     `json:"record"`    // 命中的那棵树基本信息
	RootID   int        `json:"root_id"`   // 根节点 ID
	LeafID   int        `json:"leaf_id"`   // 命中叶子节点 ID
	NodePath []int      `json:"node_path"` // 节点 ID 路径：例如 [1, 3, 5]
	EdgePath []PathStep `json:"edge_path"` // 正序排列的完整推理链条
}

// 查询引擎（带内存缓存，避免每次查都重新读磁盘）
type Engine struct {
	mu      sync.RWMutex
	Records []Record         // 👈 改为大写 Records（公开给外部访问）
	UUIDMap map[int64]Record // 👈 改为大写 UUIDMap（公开给外部访问）
}

// NewEngine 初始化并从 bin 文件加载数据
func NewEngine(binPath string) (*Engine, error) {
	file, err := os.Open(binPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []Record
	uuidMap := make(map[int64]Record)
	decoder := msgpack.NewDecoder(file)

	for {
		var rec Record
		err := decoder.Decode(&rec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
		uuidMap[rec.UUID] = rec
	}

	return &Engine{
		Records: records, // 👈 对应大写的 Records
		UUIDMap: uuidMap, // 👈 对应大写的 UUIDMap
	}, nil
}

// Query 在前 topK 棵逻辑树中，通过 keyword 检索并反向回溯完整的思考链路
func (e *Engine) Query(ctx context.Context, topK int, keyword string) (*QueryResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1. 边界保护：限制 topK 范围
	limit := topK
	if limit <= 0 || limit > len(e.Records) {
		limit = len(e.Records)
	}

	// 2. 遍历前 topK 棵树进行 Keyword 匹配
	var matchedRecord *Record
	for i := 0; i < limit; i++ {
		// 优先响应外部 ctx 超时或取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rec := &e.Records[i]
		// 👇👇👇 替换为这一段智能清洗与双向匹配 👇👇👇
		// 1. 过滤掉前后的空格、中英文引号
		cleanInput := strings.Trim(strings.ToLower(keyword), " \t\r\n\"'“”‘’")
		cleanRecKey := strings.Trim(strings.ToLower(rec.Keyword), " \t\r\n\"'“”‘’")

		if cleanRecKey == "" {
			continue
		}

		// 2. 双向包含：只要用户提问包含了关键词，或者关键词包含用户提问
		if strings.Contains(cleanInput, cleanRecKey) || strings.Contains(cleanRecKey, cleanInput) {
			matchedRecord = rec
			break
		}
		// 👆👆👆 替换结束 👆👆👆
	}

	if matchedRecord == nil {
		return nil, errors.New("在前 topK 棵树中未找到匹配关键词的对话记录")
	}

	entries := matchedRecord.Matrix.Entries
	if len(entries) == 0 {
		return nil, errors.New("该记录中没有连线拓扑信息")
	}

	// 3. 构建 end_id -> Entry 的反向映射表（为了 O(1) 逆向寻根）
	parentLookup := make(map[int]Entry, len(entries))
	for _, entry := range entries {
		parentLookup[entry.EndID] = entry
	}

	// 4. 定位目标起点：按你的逻辑，keyword 是最后一条边的 end_id
	targetLeafID := entries[len(entries)-1].EndID

	// 5. 逆向回溯寻根（不断往左找 parent，直到找不到了为止）
	var reversedSteps []PathStep
	currID := targetLeafID

	for {
		// 每次回溯也做一次 ctx 响应
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		edge, hasParent := parentLookup[currID]
		if !hasParent {
			// 当前节点不在任何边集的 end_id 里，说明它就是最顶层的根节点（Root）！
			break
		}

		// 提取该步的对话内容
		pText, cText := "", ""
		if len(edge.RawData) > 0 {
			pText = edge.RawData[0]
		}
		if len(edge.RawData) > 1 {
			cText = edge.RawData[1]
		}

		reversedSteps = append(reversedSteps, PathStep{
			ParentID:   edge.StartID,
			ChildID:    edge.EndID,
			ParentText: pText,
			ChildText:  cText,
		})

		// 往左跳一步
		currID = edge.StartID
	}

	rootID := currID // 最终定格在 root

	// 6. 将逆序链路反转成正向演变链：[Root -> ... -> Leaf]
	stepCount := len(reversedSteps)
	orderedSteps := make([]PathStep, stepCount)
	nodePath := make([]int, 0, stepCount+1)

	nodePath = append(nodePath, rootID)
	for i := 0; i < stepCount; i++ {
		step := reversedSteps[stepCount-1-i]
		orderedSteps[i] = step
		nodePath = append(nodePath, step.ChildID)
	}

	return &QueryResult{
		Record:   *matchedRecord,
		RootID:   rootID,
		LeafID:   targetLeafID,
		NodePath: nodePath,
		EdgePath: orderedSteps,
	}, nil
}
