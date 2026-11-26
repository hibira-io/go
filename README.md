# Hibira for Go

Go packages for [Hibira](https://hibira.io). One module, no dependencies,
Go 1.21 or later.

```sh
go get github.com/hibira-io/go
```

| Package | What it does |
| --- | --- |
| [`github.com/hibira-io/go/approvals`](approvals/) | Ask a person for a decision from a server, job or agent with [Hibira Approvals](https://hibira.io/approvals), wait for the answer and verify decision callbacks. |
| [`github.com/hibira-io/go/waitingroom`](waitingroom/) | Check [waiting room](https://hibira.io/waiting-room) passes and put the queue in front of any `net/http` handler (chi, Echo, Gin and others). |

```go
import (
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/waitingroom"
)
```

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

The waiting room tests check a pass signed by the Hibira server, the same
test vector hibira-io/waiting-room uses. If the pass format changes, update
it in both places.

## License

MIT

## 中文摘要

Hibira 的 Go 套件集中在這個 repo，一個模組（`github.com/hibira-io/go`），沒有外部相依，需要 Go 1.21 以上。`approvals` 子套件透過託管的 Approvals API 請人做決定、等待結果並驗證 callback；`waitingroom` 子套件驗證等候室 pass，並提供 `net/http` middleware。版本用一般的 `vX.Y.Z` tag 與 GitHub Release 發布，所有子套件共用同一個版本號。之後的 Chat API、Live Chat 套件也會放在這裡。
