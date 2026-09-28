package storage

import (
	"sync"
	"testing"
	"time"
)

func TestChatLockerSerializesSameChat(t *testing.T) {
	locker := NewChatLocker()
	firstAcquired := make(chan struct{})
	secondAcquired := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		release := locker.Acquire("chat-a")
		close(firstAcquired)
		time.Sleep(20 * time.Millisecond)
		release()
	}()
	go func() {
		defer wg.Done()
		<-firstAcquired
		release := locker.Acquire("chat-a")
		close(secondAcquired)
		release()
	}()

	select {
	case <-secondAcquired:
		t.Fatal("same chat acquired concurrently")
	case <-time.After(5 * time.Millisecond):
	}
	wg.Wait()
}

func TestChatLockerAllowsDifferentChats(t *testing.T) {
	locker := NewChatLocker()
	releaseA := locker.Acquire("chat-a")
	defer releaseA()

	acquired := make(chan struct{})
	go func() {
		releaseB := locker.Acquire("chat-b")
		close(acquired)
		releaseB()
	}()

	select {
	case <-acquired:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("different chat was blocked")
	}
}
