package hibira_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/stream"
)

// TestE2E runs against a real server when HIBIRA_E2E=1 and the HIBIRA_*
// settings point at it, such as a local pusher-server in STANDALONE_MODE.
func TestE2E(t *testing.T) {
	if os.Getenv("HIBIRA_E2E") != "1" {
		t.Skip("set HIBIRA_E2E=1 and HIBIRA_* to run against a server")
	}
	c := hibira.FromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Trigger(ctx, "e2e-orders", "created", map[string]any{"id": 1042, "note": "<ok> & \"quoted\""}); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if err := c.Publish(ctx, hibira.Event{Channels: []string{"e2e-a", "private-e2e-b", "presence-e2e-c"}, Name: "multi", Data: "raw"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := c.TriggerBatch(ctx, []hibira.BatchEvent{{Channel: "e2e-a", Name: "one", Data: 1}, {Channel: "e2e-b", Name: "two", Data: "2"}}); err != nil {
		t.Fatalf("TriggerBatch: %v", err)
	}
	if _, err := c.Channels(ctx, "presence-", "user_count"); err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if info, err := c.Channel(ctx, "presence-e2e-c", "user_count", "subscription_count"); err != nil || info.Occupied {
		t.Fatalf("Channel: %+v %v", info, err)
	}
	if _, err := c.Users(ctx, "presence-e2e-c"); err != nil {
		t.Fatalf("Users: %v", err)
	}
	if err := c.Trigger(ctx, "e2e", "big", strings.Repeat("x", 300_000)); hibira.StatusOf(err) != 413 {
		t.Fatalf("oversized event: want 413, got %v", err)
	}

	s, err := stream.New(ctx, c, "private-e2e-stream", stream.Options{Meta: map[string]string{"model": "test"}, Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		_, _ = s.WriteString("token ")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("stream: %v", err)
	}

	a, err := c.Approvals().Request(ctx, approvals.Request{Title: "e2e: deploy?", Options: []string{"Deploy", "Cancel"}, ExpiresInMinutes: 1})
	var ae *approvals.Error
	switch {
	case errors.As(err, &ae) && ae.Status == 501:
		t.Logf("approvals need an app in a Hibira account: %v", err)
	case err != nil:
		t.Fatalf("approval: %v", err)
	default:
		if got, err := c.Approvals().Get(ctx, a.ID, 0); err != nil || !got.Pending() {
			t.Fatalf("approval get: %+v %v", got, err)
		}
	}

	conv, err := c.PutConversation(ctx, "e2e-order-1042", hibira.ConversationInput{Name: "Order", Members: []string{"ann", "bob"}})
	if err != nil {
		if st := hibira.StatusOf(err); st == 403 || st == 404 || st == 501 {
			t.Skipf("Chat API not enabled on this server: %v", err)
		}
		t.Fatalf("PutConversation: %v", err)
	}
	if len(conv.Members) != 2 {
		t.Fatalf("members %+v", conv.Members)
	}
	sent, err := c.SendMessage(ctx, conv.ID, hibira.MessageInput{UserID: "ann", Text: "hi"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	page, err := c.ListMessages(ctx, conv.ID, hibira.ListOptions{Last: 10})
	if err != nil || page.LastSeq != sent.Message.Seq || page.Messages[len(page.Messages)-1].Text != "hi" {
		t.Fatalf("ListMessages: %+v %v", page, err)
	}
	if _, err := c.MarkRead(ctx, conv.ID, "bob", sent.Message.Seq); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	ucs, err := c.UserConversations(ctx, "bob", 10, "")
	if err != nil || len(ucs.Conversations) == 0 {
		t.Fatalf("UserConversations: %+v %v", ucs, err)
	}
	if err := c.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
}
