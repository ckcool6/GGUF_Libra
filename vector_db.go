package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/philippgille/chromem-go"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
)

var (
	ragCollection *chromem.Collection
	ragOnce       sync.Once
)

type contextKey string

const embeddingURLKey contextKey = "embedding_url"

func getEmbeddingsURL(customURL string) string {
	defaultURL := "http://127.0.0.1:8021/v1/embeddings"
	if strings.TrimSpace(customURL) == "" {
		return defaultURL
	}

	u, err := url.Parse(customURL)
	if err != nil {
		return defaultURL
	}

	u.Path = "/v1/embeddings"
	u.RawQuery = ""
	return u.String()
}

func llamaEmbeddingFunc(ctx context.Context, text string) ([]float32, error) {
	apiURL := "http://127.0.0.1:8021/v1/embeddings"

	if urlVal, ok := ctx.Value(embeddingURLKey).(string); ok && urlVal != "" {
		apiURL = urlVal
	}

	payload := map[string]interface{}{
		"input": text,
		"model": "embedding",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Pass through authorization headers.
	if authVal, ok := ctx.Value("auth_header").(string); ok && authVal != "" {
		req.Header.Set("Authorization", authVal)
	}

	resp, err := httpTimeoutClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Intercept non-200 responses to throw detailed error messages.
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Embedding 响应异常 [%d]: %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		return nil, fmt.Errorf("生成向量失败: %v", err)
	}

	return result.Data[0].Embedding, nil
}

// Initialize the local embedded vector database (persisted to disk in ./chromem_db).
func initRAG() {
	ragOnce.Do(func() {
		db, err := chromem.NewPersistentDB("./chromem_db", false)
		if err != nil {
			fmt.Println("⚠️ 向量数据库初始化失败:", err)
			return
		}

		collection, err := db.GetOrCreateCollection("code_knowledge_base", nil, llamaEmbeddingFunc)
		if err != nil {
			fmt.Println("⚠️ 创建向量集合失败:", err)
			return
		}

		ragCollection = collection
		fmt.Println("[OK] Chromem-go 向量数据库已就绪 (数据目录: ./chromem_db)")
	})
}

func getLanguageByExtension(ext string) *sitter.Language {
	switch strings.ToLower(ext) {
	case ".go":
		return golang.GetLanguage()
	case ".py", ".pyw":
		return python.GetLanguage()
	case ".js", ".jsx", ".mjs", ".cjs":
		return javascript.GetLanguage()
	case ".cpp", ".cxx", ".cc", ".c", ".h", ".hpp":
		return cpp.GetLanguage()
	default:
		return nil
	}
}

// Parse code using Tree-sitter AST and extract structured snippets.
func splitCodeWithTreeSitter(content []byte, fileName string) []string {
	ext := filepath.Ext(fileName)
	lang := getLanguageByExtension(ext)

	// If the language is unsupported, fall back to plain text chunking.
	if lang == nil {
		return fallbackTextSplitter(string(content), 500)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return fallbackTextSplitter(string(content), 500)
	}

	var chunks []string
	root := tree.RootNode()

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		nodeType := child.Type()

		// Match core AST nodes (functions, classes, structs, etc.) based on the file type.
		isTargetNode := false
		switch ext {
		case ".go":
			isTargetNode = nodeType == "function_declaration" ||
				nodeType == "method_declaration" ||
				nodeType == "type_declaration"

		case ".py", ".pyw":
			isTargetNode = nodeType == "function_definition" ||
				nodeType == "class_definition"

		case ".js", ".jsx", ".mjs", ".cjs":
			isTargetNode = nodeType == "function_declaration" ||
				nodeType == "class_declaration" ||
				nodeType == "lexical_declaration" || // const / let declare
				nodeType == "export_statement"

		case ".cpp", ".cxx", ".cc", ".c", ".h", ".hpp":
			isTargetNode = nodeType == "function_definition" ||
				nodeType == "class_specifier" ||
				nodeType == "struct_specifier" ||
				nodeType == "namespace_definition"
		}

		if isTargetNode {
			chunkText := string(content[child.StartByte():child.EndByte()])
			if strings.TrimSpace(chunkText) != "" {
				chunks = append(chunks, chunkText)
			}
		}
	}

	// Fall back automatically if no eligible syntax nodes are matched.
	if len(chunks) == 0 {
		return fallbackTextSplitter(string(content), 500)
	}
	return chunks
}

func fallbackTextSplitter(text string, chunkSize int) []string {
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func addChunksToVectorDB(ctx context.Context, chunks []string, filename string) error {
	ragMu.Lock()
	col := ragCollection
	ragMu.Unlock()

	if col == nil {
		return fmt.Errorf("向量数据库未初始化")
	}

	docs := make([]chromem.Document, len(chunks))
	for i, chunk := range chunks {
		docs[i] = chromem.Document{
			ID:      fmt.Sprintf("%s_%d_%d", filename, time.Now().UnixNano(), i),
			Content: chunk,
			Metadata: map[string]string{
				"file": filename,
			},
		}
	}

	return col.AddDocuments(ctx, docs, 4)
}

func queryVectorDB(ctx context.Context, query string, topK int) ([]string, error) {
	ragMu.Lock()
	col := ragCollection
	ragMu.Unlock()

	if col == nil {
		return nil, nil
	}

	// Get the total number of documents in the current collection.
	docCount := col.Count()
	if docCount == 0 {
		return nil, nil
	}

	// Dynamically cap topK to ensure it does not exceed the actual document count.
	if topK > docCount {
		topK = docCount
	}

	results, err := col.Query(ctx, query, topK, nil, nil)
	if err != nil {
		return nil, err
	}

	var matchedTexts []string
	for _, res := range results {
		matchedTexts = append(matchedTexts, res.Content)
	}
	return matchedTexts, nil
}

var ragMu sync.Mutex

// ClearVectorDB clears the in-memory collections and permanently removes persisted data and all .gob files from disk.
func ClearVectorDB() error {
	ragMu.Lock()
	defer ragMu.Unlock()

	// Clear in-memory objects to release references.
	ragCollection = nil

	// Delete the persistence directory generated by chromem-go.
	if err := os.RemoveAll("./chromem_db"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 ./chromem_db 目录失败: %w", err)
	}

	// Scan and remove all .gob files in the current working directory.
	gobFiles, err := filepath.Glob("*.gob")
	if err == nil {
		for _, file := range gobFiles {
			_ = os.Remove(file)
		}
	}

	// Reinitialize a new empty PersistentDB and Collection to ensure incoming documents can be accepted.
	db, err := chromem.NewPersistentDB("./chromem_db", false)
	if err != nil {
		return fmt.Errorf("重新创建 PersistentDB 失败: %w", err)
	}

	collection, err := db.GetOrCreateCollection("code_knowledge_base", nil, llamaEmbeddingFunc)
	if err != nil {
		return fmt.Errorf("重新创建 Collection 失败: %w", err)
	}

	ragCollection = collection
	return nil
}
