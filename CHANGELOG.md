# Changelog

## Unreleased

- First release of `github.com/hibira-io/go`, the Go packages for Hibira,
  moved here from the `go/` folders of hibira-io/approvals and
  hibira-io/waiting-room (those were never tagged):
  - `github.com/hibira-io/go/approvals`: ask a person for a decision through
    the hosted approvals API and verify decision callbacks.
  - `github.com/hibira-io/go/waitingroom`: waiting room pass check,
    `SignUser` and a `net/http` middleware.
