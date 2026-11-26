# Hibira for Gin

`github.com/hibira-io/go/gin` connects [Hibira](https://hibira.io) to Gin: channel authorization and
user sign-in endpoints for pusher-js, webhook and Approvals callback
receivers, and a waiting room middleware. Your callbacks get Gin's own
context, so whatever your auth middleware stored there is at hand.

```sh
go get github.com/hibira-io/go/gin
```

Go 1.21 or later. The core client is
[`github.com/hibira-io/go`](../); this module adds only the Gin glue.

```go
client := hibira.FromEnv()
r := gin.Default()

api := r.Group("/hibira", requireLogin)
api.POST("/auth", hibiragin.Auth(client, func(c *gin.Context, channel string) (bool, *hibira.Member) {
	u := c.MustGet("user").(*User)
	if channel == "private-user-"+u.ID {
		return true, nil
	}
	return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: gin.H{"name": u.Name}}
}))
api.POST("/user-auth", hibiragin.UserAuth(client, func(c *gin.Context) *hibira.User {
	return &hibira.User{ID: c.MustGet("user").(*User).ID}
}))

r.POST("/hibira/webhook", hibiragin.Webhook(client, func(c *gin.Context, hook *hibira.Webhook) error {
	for _, e := range hook.Events {
		log.Println(e.Name, e.Channel)
	}
	return nil
}))
r.POST("/hibira/approvals", hibiragin.ApprovalCallback(os.Getenv("HIBIRA_APP_SECRET"), func(c *gin.Context, e *approvals.Event) error {
	return markDecided(e.Data.Approval.ID, e.Data.Approval.Status)
}))

room := waitingroom.Guard{AppKey: os.Getenv("HIBIRA_APP_KEY"), Secret: []string{os.Getenv("HIBIRA_APP_SECRET")}, Room: "presale"}
r.GET("/drop", hibiragin.WaitingRoom(room), func(c *gin.Context) {
	c.String(200, "Your number: %d", hibiragin.Pass(c).Number)
})
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

## 中文摘要

Gin 專用模組：pusher-js 的頻道授權與使用者登入端點、webhook 與 Approvals callback 驗證、等候室 middleware。callback 拿到 `*gin.Context`，可以直接讀取登入 middleware 存的使用者。需要 Go 1.21 以上。
