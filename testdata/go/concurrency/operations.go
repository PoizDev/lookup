package concurrency

import "sync"

func Risky(group *sync.WaitGroup, values []string) {
	for range values {
		go func() { group.Add(1) }()
	}
}
