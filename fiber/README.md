# Hibira for Fiber

`github.com/hibira-io/go/fiber` connects [Hibira](https://hibira.io) to Fiber (v3): channel authorization and
user sign-in endpoints for pusher-js, webhook and Approvals callback
receivers, and a waiting room middleware. Your callbacks get Fiber's own
context, so whatever your auth middleware stored there is at hand.

```sh
go get github.com/hibira-io/go/fiber
```

Go 1.25 or later. The core client is
[`github.com/hibira-io/go`](../); this module adds only the Fiber glue.

```go
client := hibira.FromEnv()
app := fiber.New()

g := app.Group("/hibira", requireLogin)
g.Post("/auth", hibirafiber.Auth(client, func(c fiber.Ctx, channel string) (bool, *hibira.Member) {
	u := c.Locals("user").(*User)
	if channel == "private-user-"+u.ID {
		return true, nil
	}
	return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: fiber.Map{"name": u.Name}}
}))
g.Post("/user-auth", hibirafiber.UserAuth(client, func(c fiber.Ctx) *hibira.User {
	return &hibira.User{ID: c.Locals("user").(*User).ID}
}))

app.Post("/hibira/webhook", hibirafiber.Webhook(client, onWebhook))
app.Post("/hibira/approvals", hibirafiber.ApprovalCallback(os.Getenv("HIBIRA_APP_SECRET"), onDecision))

room := waitingroom.Guard{AppKey: os.Getenv("HIBIRA_APP_KEY"), Secret: []string{os.Getenv("HIBIRA_APP_SECRET")}, Room: "presale"}
app.Get("/drop", hibirafiber.WaitingRoom(room), func(c fiber.Ctx) error {
	return c.SendString(fmt.Sprintf("Your number: %d", hibirafiber.Pass(c).Number))
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

## Fiber v2

Mount the `net/http` handlers of the core module with the adaptor
middleware: `app.Post("/hibira/auth", adaptor.HTTPHandler(client.AuthHandler(fn)))`.

## 中文摘要

Fiber v3 專用模組：頻道授權、使用者登入、webhook、Approvals callback 與等候室 middleware，callback 拿到 `fiber.Ctx`。需要 Go 1.25 以上。Fiber v2 請用核心模組的 `net/http` handler 搭配 `adaptor.HTTPHandler`。
