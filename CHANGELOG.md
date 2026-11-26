# Changelog

## Unreleased

- New core package `github.com/hibira-io/go` (`hibira.Client`): publish
  events (`Trigger`, `Publish`, `TriggerBatch`), channel info (`Channels`,
  `Channel`, `Users`), `TerminateUserConnections`, channel authorization and
  user sign-in (`AuthorizeChannel`, `AuthenticateUser`), webhook
  verification, push notifications (`PushToInterests`, `PushToUsers`,
  `PushToken`, `DeletePushUser`) and the Chat API. `net/http` handlers
  `AuthHandler`, `UserAuthHandler` and `WebhookHandler`. `FromEnv` reads the
  `HIBIRA_*` settings. Still no dependencies.
- New `github.com/hibira-io/go/stream`: stream LLM output to a channel for
  `useStream()` (`io.Writer`, batching, size limit, final text).
- New `github.com/hibira-io/go/livechat`: `UserHash` and `Snippet` for Live
  Chat verified visitors.
- New framework modules, tagged separately: `github.com/hibira-io/go/gin`
  (Gin), `github.com/hibira-io/go/echo` (Echo v5) and
  `github.com/hibira-io/go/fiber` (Fiber v3), each with `Auth`, `UserAuth`,
  `Webhook`, `ApprovalCallback` and `WaitingRoom`.

## v0.1.0

- First release of `github.com/hibira-io/go`, the Go packages for Hibira,
  moved here from the `go/` folders of hibira-io/approvals and
  hibira-io/waiting-room (those were never tagged):
  - `github.com/hibira-io/go/approvals`: ask a person for a decision through
    the hosted approvals API and verify decision callbacks.
  - `github.com/hibira-io/go/waitingroom`: waiting room pass check,
    `SignUser` and a `net/http` middleware.
