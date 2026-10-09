package common

import (
	"bytes"
	"sync"
	"testing"
)

func TestPTYBufferSignalsOncePerClear(t *testing.T) {
	var b PTYBuffer
	gen := b.Generation()
	if !b.AppendFrom(gen, []byte("a")) {
		t.Fatal("first append must ask the reader to signal the UI")
	}
	if b.AppendFrom(gen, []byte("b")) {
		t.Fatal("a signal is already outstanding; a second one only adds queue pressure")
	}
	b.ClearSignal()
	if !b.AppendFrom(gen, []byte("c")) {
		t.Fatal("after the UI took the signal, new output must signal again")
	}
	if got := b.Peek(10); string(got) != "abc" {
		t.Fatalf("Peek = %q, want %q", got, "abc")
	}
}

func TestPTYBufferPeekDoesNotConsume(t *testing.T) {
	var b PTYBuffer
	b.Append([]byte("hello world"))
	if got := b.Peek(5); string(got) != "hello" {
		t.Fatalf("Peek = %q", got)
	}
	if b.Len() != 11 {
		t.Fatalf("Peek consumed bytes: Len = %d", b.Len())
	}
	b.Consume(6)
	if got := b.Peek(100); string(got) != "world" {
		t.Fatalf("after Consume, Peek = %q", got)
	}
}

// A chunk handed out by Peek must not change when the reader appends more:
// the parser may still be working through it.
func TestPTYBufferPeekReturnsACopy(t *testing.T) {
	var b PTYBuffer
	b.Append([]byte("abc"))
	chunk := b.Peek(3)
	b.Consume(3)
	b.Append([]byte("xyz"))
	if string(chunk) != "abc" {
		t.Fatalf("chunk changed under its holder: %q", chunk)
	}
}

func TestPTYBufferResetDiscardsAStaleReader(t *testing.T) {
	var b PTYBuffer
	old := b.Generation()
	b.Append([]byte("old"))
	b.Reset()
	if b.Len() != 0 {
		t.Fatal("Reset must discard buffered output")
	}
	if b.AppendFrom(old, []byte("late")) {
		t.Fatal("a stopped reader's leftovers must not signal")
	}
	if b.Len() != 0 {
		t.Fatal("a stopped reader's leftovers must not land in the new stream")
	}
	if !b.AppendFrom(b.Generation(), []byte("new")) {
		t.Fatal("the current reader must still signal after a Reset")
	}
}

func TestPTYBufferKeepsOrderUnderConcurrentAppend(t *testing.T) {
	var b PTYBuffer
	gen := b.Generation()
	var want bytes.Buffer
	for i := 0; i < 1000; i++ {
		want.WriteByte(byte('a' + i%26))
	}
	var got bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, c := range want.Bytes() {
			b.AppendFrom(gen, []byte{c})
		}
	}()
	for got.Len() < want.Len() {
		chunk := b.Peek(7)
		got.Write(chunk)
		b.Consume(len(chunk))
	}
	wg.Wait()
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatal("bytes reordered or lost between reader and consumer")
	}
}
