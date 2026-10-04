// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

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

// Query implements a two-stage search strategy:
//  1. Fast Path: Match against the terminal leaf nodes of the topK trees.
//  2. Reverse Search: If unmatched, scan backwards from the latest/deepest node across
//     intermediate nodes and safely backtrack to the tree's local root (preventing cross-tree pollution).
func (e *Engine) Query(ctx context.Context, topK int, keyword string) (*QueryResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	limit := topK
	if limit <= 0 || limit > len(e.Records) {
		limit = len(e.Records)
	}

	cleanInput := strings.Trim(strings.ToLower(keyword), " \t\r\n\"'“”‘’")
	if cleanInput == "" {
		return nil, errors.New("empty query keyword")
	}

	// =========================================================================
	// Stage 1: Fast Path - Match terminal nodes across topK trees
	// =========================================================================
	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rec := &e.Records[i]
		cleanRecKey := strings.Trim(strings.ToLower(rec.Keyword), " \t\r\n\"'“”‘’")

		// Check if keyword matches the tree's terminal summary
		if cleanRecKey != "" && (strings.Contains(cleanInput, cleanRecKey) || strings.Contains(cleanRecKey, cleanInput)) {
			if len(rec.Matrix.Entries) > 0 {
				// Matched the terminal leaf node of the current tree
				targetLeafID := rec.Matrix.Entries[len(rec.Matrix.Entries)-1].EndID
				return e.backtrackWithinTree(ctx, rec, targetLeafID)
			}
		}
	}

	// =========================================================================
	// Stage 2: Fallback - Flatten and reverse deep search from the latest node
	// =========================================================================
	// Scan backwards from the last tree and its deepest edge (prioritizes latest relevant context)
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
				pText = strings.ToLower(entry.RawData[0])
			}
			if len(entry.RawData) > 1 {
				cText = strings.ToLower(entry.RawData[1])
			}

			// 1. Check Child node (derived intermediate node or branch leaf)
			if cText != "" && strings.Contains(cText, cleanInput) {
				// Hit EndID; trigger isolated backtracking within current tree
				return e.backtrackWithinTree(ctx, rec, entry.EndID)
			}

			// 2. Check Parent node (intermediate branch point or local root)
			if pText != "" && strings.Contains(pText, cleanInput) {
				// Hit StartID; trigger isolated backtracking within current tree
				return e.backtrackWithinTree(ctx, rec, entry.StartID)
			}
		}
	}

	return nil, errors.New("no matching node found in topK trees")
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
