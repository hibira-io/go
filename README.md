# Hibira for Go

Go packages for [Hibira](https://hibira.io), hosted realtime that speaks the
Pusher Channels protocol. Stream LLM output to the browser, publish events,
authorize private and presence channels, verify webhooks, send push
notifications, use the Chat API, ask a person for an approval and put a
waiting room in front of a launch. pusher-js, Laravel Echo and the
`@hibira-io/*` front-end packages connect unchanged.

```sh
go get github.com/hibira-io/go
```

The core module has no dependencies and needs Go 1.21 or later. Gin, Echo
and Fiber each have a small module in a repository of their own
([gin](https://github.com/hibira-io/gin), [echo](https://github.com/hibira-io/echo),
[fiber](https://github.com/hibira-io/fiber)), so you only download the
framework you use.

| Package | What it does |
| --- | --- |
| [`github.com/hibira-io/go`](https://pkg.go.dev/github.com/hibira-io/go) | Client: publish events, channel info, channel authorization and user sign-in, webhooks, push notifications, Chat API. `net/http` handlers for the standard library, chi and gorilla/mux. |
| [`github.com/hibira-io/go/stream`](stream/) | Stream an LLM's output to a channel as it is written, for `useStream()` in React and Vue. |
| [`github.com/hibira-io/go/approvals`](approvals/) | Ask a person for a decision from a server, job or agent with [Hibira Approvals](https://hibira.io/approvals), wait for the answer and verify decision callbacks. |
| [`github.com/hibira-io/go/waitingroom`](waitingroom/) | Check [waiting room](https://hibira.io/waiting-room) passes and put the queue in front of any `net/http` handler. |
| [`github.com/hibira-io/go/livechat`](livechat/) | Verified visitors for the [Hibira Live Chat](https://chat.hibira.io) widget. |
| [`github.com/hibira-io/gin`](https://github.com/hibira-io/gin) | Gin handlers and middleware for all of the above. |
| [`github.com/hibira-io/echo`](https://github.com/hibira-io/echo) | Echo v5 handlers and middleware. |
| [`github.com/hibira-io/fiber`](https://github.com/hibira-io/fiber) | Fiber v3 handlers and middleware. |

## Set up

Create an app at [hibira.io/portal](https://hibira.io/portal) (the free plan
needs no card) and put its keys in the environment:

```sh
HIBIRA_APP_ID=...
HIBIRA_APP_KEY=...
HIBIRA_APP_SECRET=...
```

```go
client := hibira.FromEnv()
err := client.Trigger(ctx, "orders", "created", map[string]any{"id": 1042})
```

| Field | Environment | Default |
| --- | --- | --- |
| `AppID`, `Key`, `Secret` | `HIBIRA_APP_ID`, `HIBIRA_APP_KEY`, `HIBIRA_APP_SECRET` | |
| `Host` | `HIBIRA_HOST` (with `HIBIRA_PORT`, `HIBIRA_USE_TLS`) | `https://ws.hibira.io` |
| `PushInstanceID`, `PushSecretKey` | `HIBIRA_PUSH_INSTANCE_ID`, `HIBIRA_PUSH_SECRET_KEY` | |
| `PushEndpoint` | `HIBIRA_PUSH_ENDPOINT` | `Host` |
| `HTTPClient`, `Timeout` | | `http.DefaultClient`, 10 s |

## Stream LLM output to the browser

A `stream.Stream` is an `io.Writer`: write each text delta from your LLM SDK
as it arrives and `Close` at the end. Tokens are batched (about ten events a
second), each event stays under the plan's message size, and the last event
carries the whole text when it fits, so people who open the page late catch
up.

```go
import (
	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/stream"
	"github.com/openai/openai-go"
)

s, _ := stream.New(ctx, client, "private-conversation-"+id, stream.Options{Meta: map[string]string{"role": "assistant"}})
res := llm.Chat.Completions.NewStreaming(ctx, params)
for res.Next() {
	if c := res.Current(); len(c.Choices) > 0 {
		s.WriteString(c.Choices[0].Delta.Content)
	}
}
if err := res.Err(); err != nil {
	s.Fail(err) // useStream() shows the error
} else {
	s.Close()
}
save(id, s.Text())
```

```jsx
// React (@hibira-io/react)
const { text, done } = useStream(`private-conversation-${id}`);
```

The same loop works with any SDK that streams text deltas (Anthropic,
Gemini, Bedrock, Ollama). `stream.Copy(ctx, client, channel, deltas, opts)`
drains a `<-chan string` instead. Options: `ID`, `Event` (default
`ai-stream`), `Interval` (100 ms), `MaxBytes` (3500; Free plans allow 4 KB
per message), `Meta` and `SocketID`.

## Authorize private and presence channels

```go
mux.Handle("POST /hibira/auth", client.AuthHandler(func(r *http.Request, channel string) (bool, *hibira.Member) {
	u := currentUser(r)
	if u == nil {
		return false, nil
	}
	if channel == "private-user-"+u.ID {
		return true, nil
	}
	if strings.HasPrefix(channel, "presence-room-") {
		return true, &hibira.Member{UserID: u.ID, UserInfo: map[string]string{"name": u.Name}}
	}
	return false, nil
}))
mux.Handle("POST /hibira/user-auth", client.UserAuthHandler(func(r *http.Request) *hibira.User {
	if u := currentUser(r); u != nil {
		return &hibira.User{ID: u.ID}
	}
	return nil
}))
```

Point pusher-js at them with `channelAuthorization.endpoint` and
`userAuthentication.endpoint`. `AuthorizeChannel` and `AuthenticateUser`
sign without a handler, and make no network call.

### Gin, Echo, Fiber

```go
import hibiragin "github.com/hibira-io/gin"     // go get github.com/hibira-io/gin
import hibiraecho "github.com/hibira-io/echo"   // Echo v5
import hibirafiber "github.com/hibira-io/fiber" // Fiber v3

r.POST("/hibira/auth", hibiragin.Auth(client, func(c *gin.Context, channel string) (bool, *hibira.Member) { ... }))
r.POST("/hibira/user-auth", hibiragin.UserAuth(client, func(c *gin.Context) *hibira.User { ... }))
r.POST("/hibira/webhook", hibiragin.Webhook(client, onWebhook))
r.POST("/hibira/approvals", hibiragin.ApprovalCallback(os.Getenv("HIBIRA_APP_SECRET"), onDecision))
r.GET("/drop", hibiragin.WaitingRoom(room), dropPage) // hibiragin.Pass(c) inside
```

Each module has the same five functions, and your callbacks get the
framework's own context. On Echo v4 or Fiber v2, mount the `net/http`
handlers with `echo.WrapHandler` or Fiber's `adaptor.HTTPHandler`.

## Webhooks

```go
mux.Handle("POST /hibira/webhook", client.WebhookHandler(func(r *http.Request, hook *hibira.Webhook) error {
	for _, e := range hook.Events {
		log.Println(e.Name, e.Channel, e.UserID)
	}
	return nil // an error answers 500 and the delivery is retried
}))
```

`client.VerifyWebhook(key, signature, body)` checks a delivery anywhere
else and returns an error wrapping `hibira.ErrInvalidWebhook`.

## Channels API

```go
client.Publish(ctx, hibira.Event{Channels: []string{"a", "b"}, Name: "e", Data: data, SocketID: socketID})
client.TriggerBatch(ctx, []hibira.BatchEvent{{Channel: "a", Name: "e", Data: data}})
client.Channels(ctx, "presence-", "user_count")
client.Channel(ctx, "presence-room-7", "user_count", "subscription_count")
client.Users(ctx, "presence-room-7")
client.TerminateUserConnections(ctx, "42")
```

Data that is not a `string` or `[]byte` is sent as JSON.

## Push notifications

Turn on Push for the app in the portal and set `HIBIRA_PUSH_INSTANCE_ID` and
`HIBIRA_PUSH_SECRET_KEY`. Bodies use the Beams format.

```go
client.PushToInterests(ctx, []string{"news"}, map[string]any{
	"web":  map[string]any{"notification": map[string]any{"title": "New post", "deep_link": "https://example.com/p/1"}},
	"fcm":  map[string]any{"notification": map[string]any{"title": "New post"}},
	"apns": map[string]any{"aps": map[string]any{"alert": map[string]any{"title": "New post"}}},
})
client.PushToUsers(ctx, []string{"42"}, body)
token, _ := client.PushToken("42", 24*time.Hour) // for setUserId in the web, Android and iOS SDKs
client.DeletePushUser(ctx, "42")
```

## Chat API

Conversations with stored history, read receipts and offline push
([reference](https://hibira.io/chat-api)). Clients receive `chat.*` events
on `#server-to-user-<user_id>` after `pusher.signin()`, so add the user
sign-in endpoint above.

```go
client.PutConversation(ctx, "order-1042", hibira.ConversationInput{Name: "Order #1042", Members: []string{"cust_7", "agent_2"}})
client.SendMessage(ctx, "order-1042", hibira.MessageInput{UserID: "agent_2", Text: "Your parcel left the warehouse.", Push: "offline"})
page, _ := client.ListMessages(ctx, "order-1042", hibira.ListOptions{After: lastSeq, Limit: 50})
client.MarkRead(ctx, "order-1042", "cust_7", page.LastSeq)
client.UserConversations(ctx, "cust_7", 20, "") // latest first, each with Unread
```

Also `GetConversation`, `DeleteConversation`, `AddMembers`, `RemoveMember`,
`SystemMessage`, `Typing`, `NewChatToken`, and `ChatRequest` for every other
call (reactions, edits, search, moderation, translation).

## Approvals

```go
a, err := client.Approvals().Request(ctx, approvals.Request{Title: "Refund order #1042 for $89?", Options: []string{"Refund", "Decline"}})
// share a.URL, or set PushUsers / Channel / Approvers, then
a, err = client.Approvals().Wait(ctx, a.ID)
if err == nil && a.Approved() {
	refund(1042)
}
```

See [approvals](approvals/) for callbacks and replies.

## Waiting rooms

```go
room := waitingroom.Guard{AppKey: os.Getenv("HIBIRA_APP_KEY"), Secret: []string{os.Getenv("HIBIRA_APP_SECRET")}, Room: "presale"}
http.Handle("/drop/", room.Middleware(dropHandler))
```

See [waitingroom](waitingroom/).

## Live Chat verified visitors

```go
tag, _ := livechat.Snippet(workspaceID, identitySecret, &livechat.Visitor{ID: u.ID, Name: u.Name, Email: u.Email})
// {{.LiveChat}} in an html/template page; or livechat.UserHash(identitySecret, u.ID)
```

## Errors

API errors are `*hibira.Error` (`Status`, `Message`, `Body`);
`hibira.StatusOf(err)` reads the status: 401/403 bad credentials, 404
unknown app or conversation, 413 the event is over the plan's message size,
429 rate limited. A missing setting wraps `hibira.ErrConfig`.

## Releases

Releases are tagged `vX.Y.Z` and listed under
[Releases](https://github.com/hibira-io/go/releases) and in
[CHANGELOG.md](CHANGELOG.md). All packages share the module version.

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

`TestE2E` runs against a real server when `HIBIRA_E2E=1` and the
`HIBIRA_*` settings point at it, such as a local pusher-server started with
`STANDALONE_MODE=true PUSHER_APP_CHAT=true`.

The waiting room tests check a pass signed by the Hibira server, the same
test vector hibira-io/waiting-room uses. If the pass format changes, update
it in both places.

## License

MIT

## 中文摘要

Hibira 的 Go 套件都在這個 repo。核心模組 `github.com/hibira-io/go` 沒有外部相依（Go 1.21 以上），提供：發送事件與頻道查詢、私人 / presence 頻道授權與使用者登入（`net/http` handler，標準函式庫、chi、gorilla/mux 皆可用）、webhook 驗證、推播通知、Chat API。子套件 `stream` 把 LLM 的輸出即時串流到頻道，React / Vue 的 `useStream()` 直接接收；`approvals` 請人做決定；`waitingroom` 等候室；`livechat` 產生 Live Chat 已驗證訪客的簽章與 script 標籤。

Gin、Echo（v5）、Fiber（v3）各有獨立的 repo 與模組（hibira-io/gin、hibira-io/echo、hibira-io/fiber），提供頻道授權、使用者登入、webhook、Approvals callback 與等候室 middleware，callback 拿到的是框架自己的 context。核心用 `vX.Y.Z` tag 發布；框架模組在各自 repo 發布，需要新核心時先發布核心。
