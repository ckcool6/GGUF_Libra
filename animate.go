package main

import (
	"fmt"
	"time"
)

// StartLoading starts a loading spinner animation and returns a channel to stop it
func StartLoading(message string) chan struct{} {
	stopChan := make(chan struct{})

	go func() {
		// Animation frames
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-stopChan:
				// Stop signal received: clear the current line and exit
				fmt.Print("\r\033[K")
				return
			default:
				// \r returns cursor to line start; \033[K clears content after cursor
				fmt.Printf("\r\033[K\033[36m%s\033[0m %s", frames[i%len(frames)], message)
				i++
				time.Sleep(100 * time.Millisecond) // Refresh every 100ms
			}
		}
	}()

	return stopChan
}
