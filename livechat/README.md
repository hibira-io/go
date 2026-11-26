# livechat

Server helpers for [Hibira Live Chat](https://chat.hibira.io).

Verified visitors: sign the signed-in user's ID with the workspace's identity
secret (Live Chat › Settings › Widget › Verified visitors), so the chat shows
your team who they are and nobody can pretend to be them.

```go
// The whole script tag, for an html/template page ({{.LiveChat}}):
tag, err := livechat.Snippet(workspaceID, os.Getenv("HIBIRA_LIVECHAT_IDENTITY_SECRET"),
	&livechat.Visitor{ID: u.ID, Name: u.Name, Email: u.Email})

// Or only the hash, for data-user-hash or HibiraLiveChat.identify({id, hash}):
hash, err := livechat.UserHash(identitySecret, u.ID)
```

`Snippet` with a nil visitor gives the guest widget.

## 中文摘要

Hibira Live Chat 的伺服器端輔助函式。`UserHash` 用工作區的身分密鑰對使用者 ID 簽章（`data-user-hash`），`Snippet` 直接產生整段 widget script 標籤，讓已登入的使用者以本人身分聊天。
