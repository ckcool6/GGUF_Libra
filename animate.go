// Copyright 2026 Lu ZhiYuan
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"time"
)

// StartLoading starts a loading spinner animation and returns a stop function
func StartLoading(message string) func() {
	stopChan := make(chan struct{})
	doneChan := make(chan struct{})

	go func() {
		defer close(doneChan)
		frames := []string{"|", "/", "-", "\\"}
		i := 0
		for {
			select {
			case <-stopChan:
				return
			default:
				fmt.Printf("\r\033[K%s \033[1;36m[WAIT]\033[0m [\033[36m%s\033[0m] %s", uptime(), frames[i%len(frames)], message)
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	return func() {
		close(stopChan)
		<-doneChan
	}
}
