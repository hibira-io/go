package stream

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	hibira "github.com/hibira-io/go"
)

type fake struct {
	mu     sync.Mutex
	events []map[string]any
	sizes  []int
	fail   error
}

func (f *fake) Publish(_ context.Context, e hibira.Event) error {
	b, _ := json.Marshal(e.Data)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if e.Name != Event || e.Channels[0] != "private-c" {
		panic("wrong event")
	}
	f.events = append(f.events, m)
	f.sizes = append(f.sizes, jsonSize(e.Data))
	return nil
}

func rebuild(t *testing.T, events []map[string]any) string {
	t.Helper()
	var b strings.Builder
	for i, e := range events {
		if int(e["seq"].(float64)) != i {
			t.Fatalf("event %d has seq %v", i, e["seq"])
		}
		if d, ok := e["delta"].(string); ok {
			b.WriteString(d)
		}
	}
	return b.String()
}

func TestBatchesAndFinal(t *testing.T) {
	f := &fake{}
	s, err := New(context.Background(), f, "private-c", Options{ID: "s1", Meta: map[string]string{"role": "assistant"}, Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"Hel", "lo", ", ", "wor", "ld"} {
		_, _ = s.WriteString(tok)
	}
	time.Sleep(60 * time.Millisecond)
	_, _ = s.WriteString("!")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if len(f.events) != 3 {
		t.Fatalf("want 2 batches and a final event, got %v", f.events)
	}
	if f.events[0]["meta"] == nil || f.events[1]["meta"] != nil {
		t.Fatal("meta only on the first event")
	}
	last := f.events[2]
	if last["done"] != true || last["text"] != "Hello, world!" || last["id"] != "s1" {
		t.Fatalf("final %v", last)
	}
	if rebuild(t, f.events) != "Hello, world!" || s.Text() != "Hello, world!" || s.Events() != 3 {
		t.Fatal("text")
	}
	if _, err := s.WriteString("x"); !errors.Is(err, ErrClosed) {
		t.Fatal("write after close")
	}
}

func TestSizeLimitAndUTF8(t *testing.T) {
	f := &fake{}
	s, _ := New(context.Background(), f, "private-c", Options{MaxBytes: 200, Interval: time.Hour})
	text := strings.Repeat("串流\"<", 100)
	b := []byte(text)
	// Write in 1-byte pieces so characters are split across writes.
	for i := range b {
		_, _ = s.Write(b[i : i+1])
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for i, n := range f.sizes {
		if n > 200 {
			t.Fatalf("event %d is %d bytes", i, n)
		}
	}
	if got := rebuild(t, f.events); got != text {
		t.Fatalf("rebuilt %q", got)
	}
	if _, ok := f.events[len(f.events)-1]["text"]; ok {
		t.Fatal("final event should leave out text that does not fit")
	}
}

func TestFailAndPublishError(t *testing.T) {
	f := &fake{}
	s, _ := New(context.Background(), f, "private-c", Options{})
	_, _ = s.WriteString("partial")
	_ = s.Fail(errors.New("model overloaded"))
	last := f.events[len(f.events)-1]
	if last["error"] != "model overloaded" || last["text"] != "partial" {
		t.Fatalf("%v", last)
	}

	bad := &fake{fail: errors.New("413")}
	s, _ = New(context.Background(), bad, "private-c", Options{MaxBytes: 100})
	_, _ = s.WriteString(strings.Repeat("x", 200))
	if _, err := s.WriteString("y"); err == nil || err.Error() != "413" {
		t.Fatalf("want the publish error, got %v", err)
	}
	if err := s.Close(); err == nil {
		t.Fatal("close should report it too")
	}
}

func TestCopy(t *testing.T) {
	f := &fake{}
	ch := make(chan string)
	go func() {
		for _, d := range []string{"a", "b", "c"} {
			ch <- d
		}
		close(ch)
	}()
	s, err := Copy(context.Background(), f, "private-c", ch, Options{})
	if err != nil || s.Text() != "abc" {
		t.Fatalf("%v %q", err, s.Text())
	}

	f = &fake{}
	ctx, cancel := context.WithCancel(context.Background())
	ch = make(chan string)
	go func() { ch <- "a"; cancel() }()
	s, err = Copy(ctx, f, "private-c", ch, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if last := f.events[len(f.events)-1]; last["error"] != "aborted" {
		t.Fatalf("%v", last)
	}
	_ = s
}
