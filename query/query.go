// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package query

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	mu      sync.RWMutex
	Records []Record         // Exported Records for external access
	UUIDMap map[int64]Record // Exported UUIDMap for external access
}

// NewEngine initializes and loads data from a binary file
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
		Records: records,
		UUIDMap: uuidMap,
	}, nil
}

// isClauseDelimiter identifies true sentence/clause boundaries:
// Commas, periods, exclamation/question marks, colons, semicolons, and line breaks.
// NOTE: Apostrophes (') and hyphens (-) MUST NOT be delimiters, as they belong to words like "Green's" or "Navier-Stokes".
func isClauseDelimiter(r rune) bool {
	switch r {
	// ASCII sentence & clause terminators
	case ',', '.', '!', '?', ';', ':', '\n', '\r':
		return true

	// Full-width CJK sentence & clause terminators
	case '，', '。', '！', '？', '；', '：', '、':
		return true

	default:
		return false
	}
}

// splitIntoClauses segments any text into individual clauses using universal punctuation boundaries.
func splitIntoClauses(s string) []string {
	s = strings.ToLower(s)
	return strings.FieldsFunc(s, isClauseDelimiter)
}

// cleanClause trims surrounding spaces, quotes, Markdown asterisks, and edge punctuation.
func cleanClause(s string) string {
	s = strings.ToLower(s)
	return strings.TrimFunc(s, func(r rune) bool {
		return isClauseDelimiter(r) ||
			r == ' ' || r == '\t' || r == '\n' || r == '\r' ||
			r == '"' || r == '\'' || r == '“' || r == '”' || r == '‘' || r == '’' ||
			r == '*' || r == '_' || r == '`' || r == '(' || r == ')' || r == '[' || r == ']'
	})
}

// matchFullClause enforces a strict complete-clause matching contract:
// The query must match an ENTIRE clause from the target text.
// Isolated keywords or fragments (e.g., "vector", "Green's Theorem") will not trigger a match.
func matchFullClause(targetText, query string) bool {
	cleanQ := cleanClause(query)
	if cleanQ == "" {
		return false
	}

	qNoSpace := strings.ReplaceAll(cleanQ, " ", "")
	targetClauses := splitIntoClauses(targetText)

	for _, clause := range targetClauses {
		cleanC := cleanClause(clause)
		if cleanC == "" {
			continue
		}

		// 1. Direct clause equality (e.g., exact English clause match)
		if cleanQ == cleanC {
			return true
		}

		// 2. Whitespace-agnostic clause equality
		// Solves cross-language space variations (e.g., "perl 语言的知识" == "perl语言的知识")
		cNoSpace := strings.ReplaceAll(cleanC, " ", "")
		if qNoSpace == cNoSpace {
			return true
		}
	}

	return false
}

func (e *Engine) Query(ctx context.Context, topK int, keyword string) (*QueryResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	total := len(e.Records)
	if total == 0 {
		return nil, errors.New("empty database")
	}

	cleanInput := strings.TrimSpace(keyword)
	if cleanInput == "" {
		return nil, errors.New("empty query keyword")
	}

	// =========================================================================
	// Dynamic 15% Memory Retention
	// =========================================================================
	// 1. Dynamically compute the top 15% active memory capacity
	retentionRatio := 0.15
	limit := int(float64(total) * retentionRatio)

	// 2. Cold-start floor: ensure at least minKeep records are retained when
	// the dataset is small (prevents limit from dropping to 0)
	const minKeep = 3
	if limit < minKeep {
		limit = minKeep
	}
	if limit > total {
		limit = total
	}

	// 3. Compatibility fallback: respect caller-specified topK if it is stricter than the 15% ratio
	if topK > 0 && topK < limit {
		limit = topK
	}
	logDebug("[DEBUG Query] total: %d, topK: %d, calculated limit: %d\n", total, topK, limit)

	// =========================================================================
	// Stage 1: Fast Path - Match terminal summaries in the active 15% memory pool
	// =========================================================================
	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rec := &e.Records[i]

		// Must match an entire sentence/clause within rec.Keyword
		if matchFullClause(rec.Keyword, cleanInput) {
			if len(rec.Matrix.Entries) > 0 {
				targetLeafID := rec.Matrix.Entries[len(rec.Matrix.Entries)-1].EndID
				return e.backtrackWithinTree(ctx, rec, targetLeafID)
			}
		}
	}

	// =========================================================================
	// Stage 2: Fallback - Reverse deep search across intermediate nodes
	// =========================================================================
	// Strictly confined to the [0, limit) active memory window
	for i := limit - 1; i >= 0; i-- {
		rec := &e.Records[i]
		entries := rec.Matrix.Entries

		// Traverse all directed edges of the current tree in reverse order
		for j := len(entries) - 1; j >= 0; j-- {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			entry := entries[j]
			pText, cText := "", ""
			if len(entry.RawData) > 0 {
				pText = entry.RawData[0]
			}
			if len(entry.RawData) > 1 {
				cText = entry.RawData[1]
			}

			// 1. Check child node text (must match full clause)
			if cText != "" && matchFullClause(cText, cleanInput) {
				// Hit EndID; trigger isolated backtracking within current tree
				return e.backtrackWithinTree(ctx, rec, entry.EndID)
			}

			// 2. Check parent node text (must match full clause)
			if pText != "" && matchFullClause(pText, cleanInput) {
				// Hit StartID; trigger isolated backtracking within current tree
				return e.backtrackWithinTree(ctx, rec, entry.StartID)
			}
		}
	}

	return nil, errors.New("no full-clause match found in active 15% memory")
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
