package hibira_test

import (
	"context"
	"log"
	"net/http"
	"strings"

	hibira "github.com/hibira-io/go"
)

func Example() {
	client := hibira.FromEnv() // HIBIRA_APP_ID, HIBIRA_APP_KEY, HIBIRA_APP_SECRET
	ctx := context.Background()
	if err := client.Trigger(ctx, "orders", "created", map[string]any{"id": 1042}); err != nil {
		log.Print(err)
	}
}

func ExampleClient_AuthHandler() {
	client := hibira.FromEnv()
	currentUserID := func(r *http.Request) string { return r.Header.Get("X-User") } // your session lookup

	mux := http.NewServeMux()
	mux.Handle("/hibira/auth", client.AuthHandler(func(r *http.Request, channel string) (bool, *hibira.Member) {
		id := currentUserID(r)
		switch {
		case id == "":
			return false, nil
		case channel == "private-user-"+id:
			return true, nil
		case strings.HasPrefix(channel, "presence-room-"):
			return true, &hibira.Member{UserID: id, UserInfo: map[string]string{"name": "Ann"}}
		}
		return false, nil
	}))
	mux.Handle("/hibira/user-auth", client.UserAuthHandler(func(r *http.Request) *hibira.User {
		if id := currentUserID(r); id != "" {
			return &hibira.User{ID: id}
		}
		return nil
	}))
	mux.Handle("/hibira/webhook", client.WebhookHandler(func(r *http.Request, hook *hibira.Webhook) error {
		for _, e := range hook.Events {
			log.Println(e.Name, e.Channel)
		}
		return nil
	}))
}

func ExampleClient_SendMessage() {
	client := hibira.FromEnv()
	ctx := context.Background()
	_, _ = client.PutConversation(ctx, "order-1042", hibira.ConversationInput{Name: "Order #1042", Members: []string{"cust_7", "agent_2"}})
	_, _ = client.SendMessage(ctx, "order-1042", hibira.MessageInput{UserID: "agent_2", Text: "Your parcel left the warehouse.", Push: "offline"})
}
