// Package stream sends an LLM's output (or any text produced bit by bit) to
// a Hibira channel as it is written. useStream() in @hibira-io/react and
// @hibira-io/vue, and the other front-end packages, put it back together.
//
// A Stream is an io.Writer, so it works with every Go LLM SDK: write each
// text delta as it arrives and Close at the end.
//
//	s := stream.New(ctx, client, "private-conversation-42", stream.Options{Meta: map[string]string{"role": "assistant"}})
//	for chunk := range deltas { // OpenAI, Anthropic, Gemini, Bedrock, Ollama...
//		s.WriteString(chunk)
//	}
//	err := s.Close() // or s.Fail(err) when the model call failed
//	save(s.Text())
//
// Tokens are batched (about ten events a second rather than one per token),
// each event stays under MaxBytes (Free plans allow 4 KB per message), events
// go out in order, and the last event carries the whole text when it fits,
// so people who open the page late catch up.
//
// Wire format, the same as streamToChannel in the JavaScript packages and
// stream_to_channel in Python: event "ai-stream", payloads
// {id, seq, delta, meta (first event only)}, then {id, seq, done: true,
// text?, error?}.
package stream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	hibira "github.com/hibira-io/go"
)

// Event is the default event name.
const Event = "ai-stream"

// ErrClosed is returned by Write after Close or Fail.
var ErrClosed = errors.New("stream: closed")

// Publisher publishes events; *hibira.Client is one.
type Publisher interface {
	Publish(ctx context.Context, e hibira.Event) error
}

// Options tune a Stream. The zero value works.
type Options struct {
	// ID of the stream, default a random UUID.
	ID string
	// Event name, default "ai-stream".
	Event string
	// Interval to gather text before sending it, default 100ms.
	Interval time.Duration
	// MaxBytes is the largest event payload, default 3500.
	MaxBytes int
	// Meta is sent with the first event (model, message id, author).
	Meta any
	// SocketID leaves out that connection (the sender's own tab).
	SocketID string
}

// Stream sends text to one channel. Its methods are safe for concurrent use.
type Stream struct {
	ctx     context.Context
	pub     Publisher
	channel string
	o       Options
	budget  int

	mu      sync.Mutex
	buf     strings.Builder
	partial []byte
	text    strings.Builder
	timer   *time.Timer
	closed  bool
	seq     int
	sendMu  sync.Mutex
	sendErr error
}

type payload struct {
	ID    string `json:"id"`
	Seq   int    `json:"seq"`
	Delta string `json:"delta"`
	Meta  any    `json:"meta,omitempty"`
}

type donePayload struct {
	ID    string `json:"id"`
	Seq   int    `json:"seq"`
	Done  bool   `json:"done"`
	Meta  any    `json:"meta,omitempty"`
	Error string `json:"error,omitempty"`
	Text  string `json:"text,omitempty"`
}

func jsonSize(v any) int {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return b.Len() - 1
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// New starts a stream to channel. Nothing is sent until text is written.
// ctx bounds every publish.
func New(ctx context.Context, p Publisher, channel string, o Options) (*Stream, error) {
	if o.ID == "" {
		o.ID = newID()
	}
	if o.Event == "" {
		o.Event = Event
	}
	if o.Interval <= 0 {
		o.Interval = 100 * time.Millisecond
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 3500
	}
	budget := o.MaxBytes - jsonSize(payload{ID: o.ID, Seq: 1_000_000_000, Meta: o.Meta})
	if budget < 16 {
		return nil, errors.New("stream: MaxBytes is too small for the ID and Meta")
	}
	return &Stream{ctx: ctx, pub: p, channel: channel, o: o, budget: budget}, nil
}

// ID is the stream's ID.
func (s *Stream) ID() string { return s.o.ID }

// Text is everything written so far.
func (s *Stream) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.String()
}

// Events is how many events were sent, the final one included.
func (s *Stream) Events() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// Write adds text. A UTF-8 character split across writes is put back
// together. It returns the first publish error, if any.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, ErrClosed
	}
	if err := s.err(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	data := p
	if len(s.partial) > 0 {
		data = append(s.partial, p...)
		s.partial = nil
	}
	// Keep an incomplete trailing character for the next write.
	cut := len(data)
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				cut = i
			}
			break
		}
	}
	s.partial = append([]byte(nil), data[cut:]...)
	s.add(string(data[:cut]))
	return len(p), nil
}

// WriteString adds text.
func (s *Stream) WriteString(text string) (int, error) { return s.Write([]byte(text)) }

// add appends text with s.mu held, and unlocks it.
func (s *Stream) add(text string) {
	text = strings.ToValidUTF8(text, "�")
	if text == "" {
		s.mu.Unlock()
		return
	}
	s.text.WriteString(text)
	s.buf.WriteString(text)
	if s.buf.Len() >= s.budget {
		s.flushLocked()
		return
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(s.o.Interval, s.flush)
	}
	s.mu.Unlock()
}

func (s *Stream) err() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.sendErr
}

func (s *Stream) flush() {
	s.mu.Lock()
	s.flushLocked()
}

// flushLocked sends the buffer as delta events. Called with s.mu held; it
// unlocks s.mu once the events are queued in order on s.sendMu.
func (s *Stream) flushLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	buf := s.buf.String()
	s.buf.Reset()
	if buf == "" {
		s.mu.Unlock()
		return
	}
	var out []any
	for _, d := range split(buf, s.budget) {
		p := payload{ID: s.o.ID, Seq: s.seq, Delta: d}
		if s.seq == 0 {
			p.Meta = s.o.Meta
		}
		s.seq++
		out = append(out, p)
	}
	s.sendMu.Lock()
	s.mu.Unlock()
	s.sendAll(out)
	s.sendMu.Unlock()
}

// sendAll publishes with s.sendMu held.
func (s *Stream) sendAll(out []any) {
	for _, p := range out {
		if s.sendErr != nil {
			return
		}
		s.sendErr = s.pub.Publish(s.ctx, hibira.Event{Channels: []string{s.channel}, Name: s.o.Event, Data: p, SocketID: s.o.SocketID})
	}
}

// Close sends what is left and the final event, and returns the first
// publish error.
func (s *Stream) Close() error { return s.finish("") }

// Fail ends the stream with an error that useStream() shows (err nil means
// "aborted"), and returns the first publish error.
func (s *Stream) Fail(err error) error {
	msg := "aborted"
	if err != nil && err.Error() != "" {
		msg = err.Error()
	}
	if errors.Is(err, context.Canceled) {
		msg = "aborted"
	}
	return s.finish(msg)
}

func (s *Stream) finish(errMsg string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.err()
	}
	s.closed = true
	if len(s.partial) > 0 {
		tail := strings.ToValidUTF8(string(s.partial), "�")
		s.partial = nil
		s.text.WriteString(tail)
		s.buf.WriteString(tail)
	}
	s.flushLocked()

	s.mu.Lock()
	text := s.text.String()
	d := donePayload{ID: s.o.ID, Seq: s.seq, Done: true, Error: errMsg}
	if s.seq == 0 {
		d.Meta = s.o.Meta
	}
	if text != "" {
		withText := d
		withText.Text = text
		if jsonSize(withText) <= s.o.MaxBytes {
			d = withText
		}
	}
	s.seq++
	s.sendMu.Lock()
	s.mu.Unlock()
	s.sendAll([]any{d})
	err := s.sendErr
	s.sendMu.Unlock()
	return err
}

// split cuts text so each piece's JSON-escaped size fits budget, never
// inside a character.
func split(text string, budget int) []string {
	var out []string
	var piece strings.Builder
	size := 0
	for _, r := range text {
		n := jsonSize(string(r)) - 2
		if size+n > budget && piece.Len() > 0 {
			out = append(out, piece.String())
			piece.Reset()
			size = 0
		}
		piece.WriteRune(r)
		size += n
	}
	if piece.Len() > 0 {
		out = append(out, piece.String())
	}
	return out
}

// Copy writes every string from deltas to a new stream until deltas is
// closed or ctx is done, then closes it. It returns the stream (for ID and
// Text) and the first error.
func Copy(ctx context.Context, p Publisher, channel string, deltas <-chan string, o Options) (*Stream, error) {
	s, err := New(ctx, p, channel, o)
	if err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			// Publish the final event even though ctx is done.
			s.sendMu.Lock()
			s.ctx = context.WithoutCancel(ctx)
			s.sendMu.Unlock()
			_ = s.Fail(ctx.Err())
			return s, ctx.Err()
		case d, ok := <-deltas:
			if !ok {
				return s, s.Close()
			}
			if _, err := s.WriteString(d); err != nil {
				_ = s.Fail(err)
				return s, err
			}
		}
	}
}
