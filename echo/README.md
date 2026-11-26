# Hibira for Echo

`github.com/hibira-io/go/echo` connects [Hibira](https://hibira.io) to Echo (v5): channel authorization and
user sign-in endpoints for pusher-js, webhook and Approvals callback
receivers, and a waiting room middleware. Your callbacks get Echo's own
context, so whatever your auth middleware stored there is at hand.

```sh
go get github.com/hibira-io/go/echo
```

Go 1.25 or later. The core client is
[`github.com/hibira-io/go`](../); this module adds only the Echo glue.

```go
client := hibira.FromEnv()
e := echo.New()

g := e.Group("/hibira", requireLogin)
g.POST("/auth", hibiraecho.Auth(client, func(c *echo.Context, channel string) (bool, *hibira.Member) {
	u := c.Get("user").(*User)
	if channel == "private-user-"+u.ID {
		return true, nil
	}
	return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: map[string]string{"name": u.Name}}
}))
g.POST("/user-auth", hibiraecho.UserAuth(client, func(c *echo.Context) *hibira.User {
	return &hibira.User{ID: c.Get("user").(*User).ID}
}))

e.POST("/hibira/webhook", hibiraecho.Webhook(client, onWebhook))
e.POST("/hibira/approvals", hibiraecho.ApprovalCallback(os.Getenv("HIBIRA_APP_SECRET"), onDecision))

room := waitingroom.Guard{AppKey: os.Getenv("HIBIRA_APP_KEY"), Secret: []string{os.Getenv("HIBIRA_APP_SECRET")}, Room: "presale"}
e.GET("/drop", func(c *echo.Context) error {
	return c.String(200, fmt.Sprintf("Your number: %d", hibiraecho.Pass(c).Number))
}, hibiraecho.WaitingRoom(room))
```

| Function | Does |
| --- | --- |
| `Auth(client, fn)` | Answers pusher-js channel authorization (`channelAuthorization.endpoint`). `fn` returns whether the user may subscribe, and the member for presence- channels. |
| `UserAuth(client, fn)` | Answers user sign-in (`pusher.signin()`), which the Chat API and server-to-user events need. `fn` returns the user, or nil. |
| `Webhook(client, fn)` | Verifies webhook deliveries (401 when forged) and passes them to `fn`; an error from `fn` answers 500 so the delivery is retried. |
| `ApprovalCallback(secret, fn)` | Verifies Approvals `callback_url` deliveries (`Hibira-Signature`) and passes the decision to `fn`. |
| `WaitingRoom(guard)` | Sends visitors without a pass to the hosted queue page (403 for requests other than GET or HEAD); `Pass(c)` returns the pass. |

## Stream LLM output from a handler

```go
s, _ := stream.New(ctx, client, "private-conversation-"+id, stream.Options{})
for delta := range deltas { // text deltas from your model SDK
	s.WriteString(delta)
}
s.Close()
```

See [stream](../stream/) and the [main README](../README.md) for publishing,
push notifications and the Chat API.

## Echo v4

Mount the `net/http` handlers of the core module:
`e.POST("/hibira/auth", echo.WrapHandler(client.AuthHandler(fn)))` and
`echo.WrapMiddleware(room.Middleware)`.

## 中文摘要

Echo v5 專用模組：頻道授權、使用者登入、webhook、Approvals callback 與等候室 middleware，callback 拿到 `*echo.Context`。需要 Go 1.25 以上。Echo v4 請用核心模組的 `net/http` handler 搭配 `echo.WrapHandler`。
