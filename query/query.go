// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package query

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

var LogFunc func(format string, a ...any)

func logDebug(format string, a ...any) {
	if LogFunc != nil {
		LogFunc(format, a...)
	} else {
		fmt.Printf(format, a...)
	}
}

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

// PathStep represents a single step in the reasoning chain
type PathStep struct {
	ParentID   int    `json:"parent_id"`
	ChildID    int    `json:"child_id"`
	ParentText string `json:"parent_text"`
	ChildText  string `json:"child_text"`
}

// QueryResult represents the final query result
type QueryResult struct {
	Record   Record     `json:"record"`    // Matched tree metadata
	RootID   int        `json:"root_id"`   // Root node ID
	LeafID   int        `json:"leaf_id"`   // Matched leaf node ID
	NodePath []int      `json:"node_path"` // Node ID path: e.g., [1, 3, 5]
	EdgePath []PathStep `json:"edge_path"` // Full forward-ordered reasoning chain
}

// Engine is an in-memory cached query engine to avoid redundant disk reads
type Engine struct {
	mu          sync.RWMutex
	Records     []Record         // Exported Records for external access
	UUIDMap     map[int64]Record // Exported UUIDMap for external access
	vectorCache map[string][]float32
}

// NewEngine initializes and loads data from a binary file
func NewEngine(binPath string) (*Engine, error) {
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		f, createErr := os.Create(binPath)
		if createErr != nil {
			return nil, fmt.Errorf("failed to create default %s: %w", binPath, createErr)
		}
		f.Close()
		logDebug("\033[1;36m[INFO]\033[0m %s not found, created an empty one automatically\n", binPath)
	}

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
		Records:     records,
		UUIDMap:     uuidMap,
		vectorCache: make(map[string][]float32),
	}, nil
}

// EmbeddingFunc defines the standard signature for vector generation callbacks.
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// ==========================================
// Mathematical Helpers & Cache Access
// ==========================================

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns a normalized score in the range [-1.0, 1.0].
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// getOrFetchVector retrieves the embedding vector for a given text snippet.
// It checks the in-memory cache first; on a cache miss, it invokes the embedding API
// and caches the result without modifying underlying persistent structs.
func (e *Engine) getOrFetchVector(ctx context.Context, text string, embedFn EmbeddingFunc) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("cannot embed empty text")
	}

	// 1. Fast path: return cached vector from memory
	if vec, ok := e.vectorCache[text]; ok {
		return vec, nil
	}

	// 2. Slow path: call remote/local embedding model via HTTP
	vec, err := embedFn(ctx, text)
	if err != nil {
		return nil, err
	}

	// 3. Cache the newly computed vector in volatile memory
	if e.vectorCache == nil {
		e.vectorCache = make(map[string][]float32)
	}
	e.vectorCache[text] = vec

	return vec, nil
}

// ==========================================
// Semantic Query Engine
// ==========================================

// QuerySemantic executes dual-stage semantic retrieval using volatile embedding caches.
// The persistent structs (Entry and Record) and data.bin remain completely untouched.
func (e *Engine) QuerySemantic(
	ctx context.Context,
	query string,
	topK int,
	embedFn EmbeddingFunc,
) (*QueryResult, error) {
	// Acquire write lock because vectorCache may be populated on cache misses
	e.mu.Lock()
	defer e.mu.Unlock()

	total := len(e.Records)
	if total == 0 {
		return nil, errors.New("empty database")
	}

	if embedFn == nil {
		return nil, errors.New("embedding function cannot be nil")
	}

	// 1. Generate dense query embedding vector
	queryVec, err := embedFn(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// =========================================================================
	// Dynamic 15% Memory Retention
	// =========================================================================
	// Limit search space exclusively to the top 15% topologically complex reasoning trees
	retentionRatio := 0.15
	limit := int(float64(total) * retentionRatio)

	// Cold-start floor: guarantee at least minKeep records during initial runs
	const minKeep = 3
	if limit < minKeep {
		limit = minKeep
	}
	if limit > total {
		limit = total
	}

	// Respect caller-specified topK if stricter than the 15% retention limit
	if topK > 0 && topK < limit {
		limit = topK
	}
	logDebug("[DEBUG QuerySemantic] total: %d, topK: %d, calculated limit: %d\n", total, topK, limit)

	// Semantic similarity acceptance thresholds
	const stage1Threshold float32 = 0.70 // Stricter threshold for macroscopic topic match
	const stage2Threshold float32 = 0.67 // Tolerant threshold for microscopic step match

	// =========================================================================
	// Stage 1: Fast Path - Macroscopic semantic matching on terminal summaries
	// =========================================================================
	var bestStage1Rec *Record
	var bestStage1Sim float32 = -1.0

	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rec := &e.Records[i]
		if rec.Keyword == "" {
			continue
		}

		// Retrieve or compute vector for the Keyword string directly
		kwVec, err := e.getOrFetchVector(ctx, rec.Keyword, embedFn)
		if err != nil {
			continue
		}

		sim := cosineSimilarity(queryVec, kwVec)
		if sim > bestStage1Sim {
			bestStage1Sim = sim
			bestStage1Rec = rec
		}
	}

	// If macroscopic confidence meets threshold, backtrack full reasoning chain from the leaf
	if bestStage1Rec != nil && bestStage1Sim >= stage1Threshold {
		logDebug("[DEBUG Stage 1 Hit] Semantic hit on Keyword (sim: %.4f): %s\n", bestStage1Sim, bestStage1Rec.Keyword)
		if len(bestStage1Rec.Matrix.Entries) > 0 {
			targetLeafID := bestStage1Rec.Matrix.Entries[len(bestStage1Rec.Matrix.Entries)-1].EndID
			return e.backtrackWithinTree(ctx, bestStage1Rec, targetLeafID)
		}
	}

	// =========================================================================
	// Stage 2: Fallback - Microscopic reverse deep search across intermediate nodes
	// =========================================================================
	// Strictly confined to the [0, limit) active window, traversing reasoning steps in reverse
	var bestStage2Rec *Record
	var bestTargetNodeID int = -1
	var bestStage2Sim float32 = -1.0

	for i := limit - 1; i >= 0; i-- {
		rec := &e.Records[i]
		entries := rec.Matrix.Entries

		for j := len(entries) - 1; j >= 0; j-- {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			entry := entries[j]

			// 1. Evaluate child node text (RawData[1])
			if len(entry.RawData) > 1 && entry.RawData[1] != "" {
				cVec, err := e.getOrFetchVector(ctx, entry.RawData[1], embedFn)
				if err == nil {
					sim := cosineSimilarity(queryVec, cVec)
					if sim > bestStage2Sim {
						bestStage2Sim = sim
						bestStage2Rec = rec
						bestTargetNodeID = entry.EndID
					}
				}
			}

			// 2. Evaluate parent node text (RawData[0])
			if len(entry.RawData) > 0 && entry.RawData[0] != "" {
				pVec, err := e.getOrFetchVector(ctx, entry.RawData[0], embedFn)
				if err == nil {
					sim := cosineSimilarity(queryVec, pVec)
					if sim > bestStage2Sim {
						bestStage2Sim = sim
						bestStage2Rec = rec
						bestTargetNodeID = entry.StartID
					}
				}
			}
		}
	}

	// If an intermediate step is hit, backtrack from that specific pivot node to the local root
	if bestStage2Rec != nil && bestStage2Sim >= stage2Threshold {
		logDebug("[DEBUG Stage 2 Hit] Semantic hit on intermediate node (NodeID: %d, sim: %.4f)\n", bestTargetNodeID, bestStage2Sim)
		return e.backtrackWithinTree(ctx, bestStage2Rec, bestTargetNodeID)
	}
	logDebug("[DEBUG Miss] Below similarity thresholds: Stage1 max: %.4f (threshold %.2f), Stage2 max: %.4f (threshold %.2f)\n",
		bestStage1Sim, stage1Threshold, bestStage2Sim, stage2Threshold)
	return nil, fmt.Errorf("\033[38;2;230;130;50mMissed\033[0m (stage1 max: %.4f, stage2 max: %.4f)", bestStage1Sim, bestStage2Sim)
}

// backtrackWithinTree enforces strict tree-scoped isolation:
// The parent lookup table is sandboxed exclusively within the current Record.
// Backtracking terminates naturally upon reaching the tree's local root (in-degree 0),
// physically preventing any cross-tree leakage or ID collision.
func (e *Engine) backtrackWithinTree(ctx context.Context, rec *Record, targetNodeID int) (*QueryResult, error) {
	entries := rec.Matrix.Entries
	if len(entries) == 0 {
		return &QueryResult{
			Record:   *rec,
			RootID:   targetNodeID,
			LeafID:   targetNodeID,
			NodePath: []int{targetNodeID},
			EdgePath: []PathStep{},
		}, nil
	}

	// 1. Build local EndID -> Entry mapping strictly for the current tree
	parentLookup := make(map[int]Entry, len(entries))
	for _, entry := range entries {
		parentLookup[entry.EndID] = entry
	}

	// 2. Backtrack upwards from targetNodeID towards the local root
	var reversedSteps []PathStep
	currID := targetNodeID

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		edge, hasParent := parentLookup[currID]
		if !hasParent {
			// Boundary stop: No parent found means we have reached the local root
			// of this tree. Terminate immediately to avoid leaking into other trees.
			break
		}

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

		currID = edge.StartID
	}

	rootID := currID // Backtracking converges at the local root of this tree

	// 3. Reverse the path into forward reasoning order: [Local Root -> ... -> Target Node]
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
		Record:   *rec,
		RootID:   rootID,
		LeafID:   targetNodeID, // Semantically represents the query's targeted endpoint
		NodePath: nodePath,
		EdgePath: orderedSteps,
	}, nil
}
