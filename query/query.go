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

// Query searches within the topK logical trees by keyword and backtracks the full reasoning path
func (e *Engine) Query(ctx context.Context, topK int, keyword string) (*QueryResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Boundary check: constrain topK range
	limit := topK
	if limit <= 0 || limit > len(e.Records) {
		limit = len(e.Records)
	}

	// Iterate through topK trees to match keyword
	var matchedRecord *Record
	for i := 0; i < limit; i++ {
		// Prioritize responding to context cancellation or timeout
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rec := &e.Records[i]
		// Sanitize input and perform bidirectional substring matching
		// Strip leading and trailing whitespaces and quote marks
		cleanInput := strings.Trim(strings.ToLower(keyword), " \t\r\n\"'“”‘’")
		cleanRecKey := strings.Trim(strings.ToLower(rec.Keyword), " \t\r\n\"'“”‘’")

		if cleanRecKey == "" {
			continue
		}

		// Bidirectional match: query contains keyword or keyword contains query
		if strings.Contains(cleanInput, cleanRecKey) || strings.Contains(cleanRecKey, cleanInput) {
			matchedRecord = rec
			break
		}
	}

	if matchedRecord == nil {
		return nil, errors.New("no matching conversation record found within the topK trees")
	}

	entries := matchedRecord.Matrix.Entries
	if len(entries) == 0 {
		return nil, errors.New("record contains no edge topology information")
	}

	// Build reverse map of end_id -> Entry for O(1) backtracking towards the root
	parentLookup := make(map[int]Entry, len(entries))
	for _, entry := range entries {
		parentLookup[entry.EndID] = entry
	}

	// Locate target starting point: target keyword corresponds to the end_id of the last edge
	targetLeafID := entries[len(entries)-1].EndID

	// Backtrack to find the root node (traverse upwards along parent pointers)
	var reversedSteps []PathStep
	currID := targetLeafID

	for {
		// Check context status during each backtracking step
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		edge, hasParent := parentLookup[currID]
		if !hasParent {
			// If node is not an end_id in any edge, it is the root node
			break
		}

		// Extract conversation text for this step
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

		// Move one step up to parent
		currID = edge.StartID
	}

	rootID := currID // Backtracking terminated at root

	// Reverse the path to get forward progression: [Root -> ... -> Leaf]
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
