// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tkm          *tiktoken.Tiktoken
	systemPrompt Message
	config       SystemPromptConfig
)

func config_init() {
	var err error

	// Attempt to load saved tree-structured dialogue history from local file
	rootChain, err = LoadChainFromFile("chain_history.json")
	if err != nil || rootChain == nil {
		logPrintf("%s No history chain file found, initializing a new chain...\n", tagInfo)
		rootChain = NewChatChain()
	} else {
		logPrintf("%s Successfully loaded history chain structure\n", tagOK)
	}

	// Default currentChain to the deepest leaf node of the main branch
	currentChain = rootChain
	for currentChain.DialogMain != nil {
		currentChain = currentChain.DialogMain
	}

	stopLoading := StartLoading("Initializing token encoder (first run may require downloading vocabulary files, please wait)...")

	tkm, err = tiktoken.GetEncoding("cl100k_base")

	stopLoading()
	if err != nil {
		logPrintf("\r\033[K%s Failed to initialize token encoder: %v\n", tagError, err)
		panic(err)
	}
	logPrintf("\r\033[K%s Token encoder initialized\n", tagOK)

	err = loadSystemPrompt("system_prompt.json")
	if err != nil {
		panic(fmt.Sprintf("%s Failed to load system prompt: %v", tagError, err))
	}
	logPrintf("%s Prompt configuration file loaded successfully\n", tagOK)
}

func loadSystemPrompt(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	setActivePrompt(config.Active)
	return nil
}

func setActivePrompt(id string) bool {
	for _, p := range config.Prompts {
		if p.ID == id {
			config.Active = id
			systemPrompt = Message{
				Role:    "system",
				Content: p.Content,
			}
			return true
		}
	}
	return false
}

func getMessageTokens(msg Message) int {
	// Base tokens (Role + Content)
	tokens := 4
	tokens += len(tkm.Encode(msg.Role, nil, nil))
	tokens += len(tkm.Encode(msg.Content, nil, nil))

	// Image tokens
	if msg.Image != "" {
		// Images processed via mmproj occupy fixed visual token slots.
		// 1024 is a conservative and standard estimation.
		tokens += 1024
	}
	return tokens
}

func filterMessagesByToken(history []Message, maxTokens int) []Message {
	var result []Message
	var systemBackdrops []Message // Stores archived background summaries
	totalTokens := 0

	// Calculate tokens for the global system prompt (persona)
	if systemPrompt.Content != "" {
		totalTokens += getMessageTokens(systemPrompt)
	}

	// Preprocess: extract all archived background summaries from history (system role matching keywords)
	// These are critical context and must be retained with highest priority
	for _, msg := range history {
		if msg.Role == "system" && (strings.Contains(msg.Content, "Previous Context") || strings.Contains(msg.Content, "Context Summary")) {
			tokens := getMessageTokens(msg)
			if totalTokens+tokens <= maxTokens {
				totalTokens += tokens
				systemBackdrops = append(systemBackdrops, msg)
			}
		}
	}

	// Process regular chat turns (user / assistant) in reverse chronological order
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]

		// Skip system messages as they have already been processed above
		if msg.Role == "system" {
			continue
		}

		msgTokens := getMessageTokens(msg)

		if totalTokens+msgTokens > maxTokens {
			break // Stop including older messages once token limit is reached
		}

		totalTokens += msgTokens
		result = append(result, msg)
	}

	// Restore original chronological order (reversed from the loop above)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	// Final assembly: [Global persona] + [Archived background summaries] + [Recent dialogue turns]
	finalMessages := []Message{}
	if systemPrompt.Content != "" {
		finalMessages = append(finalMessages, systemPrompt)
	}
	finalMessages = append(finalMessages, systemBackdrops...)
	finalMessages = append(finalMessages, result...)

	return finalMessages
}
