# Changelog

## Unreleased

- The Gin, Echo and Fiber modules moved to their own repositories before
  their first release: [hibira-io/gin](https://github.com/hibira-io/gin),
  [hibira-io/echo](https://github.com/hibira-io/echo) and
  [hibira-io/fiber](https://github.com/hibira-io/fiber)
  (`github.com/hibira-io/gin` and so on).

## v0.2.0

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

## v0.1.0

- First release of `github.com/hibira-io/go`, the Go packages for Hibira,
  moved here from the `go/` folders of hibira-io/approvals and
  hibira-io/waiting-room (those were never tagged):
  - `github.com/hibira-io/go/approvals`: ask a person for a decision through
    the hosted approvals API and verify decision callbacks.
  - `github.com/hibira-io/go/waitingroom`: waiting room pass check,
    `SignUser` and a `net/http` middleware.
