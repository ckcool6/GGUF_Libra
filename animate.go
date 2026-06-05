package main

import (
	"fmt"
	"time"
)

// StartLoading 启动一个加载动画，返回一个用于停止的 channel
func StartLoading(message string) chan struct{} {
	stopChan := make(chan struct{})
	
	go func() {
		// 动画帧
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-stopChan:
				// 收到停止信号，清除当前行并退出
				fmt.Print("\r\033[K") 
				return
			default:
				// \r 让光标回到行首，\033[K 清除光标后的内容
				fmt.Printf("\r\033[K%s %s", frames[i%len(frames)], message)
				i++
				time.Sleep(100 * time.Millisecond) // 每 100 毫秒刷新一次
			}
		}
	}()
	
	return stopChan
}