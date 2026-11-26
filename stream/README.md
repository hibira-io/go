# stream

Send an LLM's output, or any text produced bit by bit, to a Hibira channel
as it is written. `useStream()` in [@hibira-io/react](https://github.com/hibira-io/react)
and [@hibira-io/vue](https://github.com/hibira-io/vue) puts it back together.

```go
s, err := stream.New(ctx, client, "private-conversation-42", stream.Options{Meta: map[string]string{"role": "assistant"}})
for delta := range deltas { // each text delta from your model SDK
	if _, err := s.WriteString(delta); err != nil {
		break // a publish failed; Close returns the same error
	}
}
err = s.Close() // or s.Fail(err) when the model call failed
save(s.ID(), s.Text())
```

A `Stream` is an `io.Writer` and safe for concurrent use. A UTF-8 character
split across writes is put back together.

| Option | Default | |
| --- | --- | --- |
| `ID` | random UUID | stream ID |
| `Event` | `ai-stream` | event name |
| `Interval` | 100 ms | time to gather text before sending it |
| `MaxBytes` | 3500 | largest event payload (Free plans allow 4 KB per message) |
| `Meta` | | JSON sent with the first event (model, message ID, author) |
| `SocketID` | | leave out this connection (the sender's own tab) |

`stream.Copy(ctx, client, channel, deltas, opts)` drains a `<-chan string`
and ends with error `aborted` when `ctx` is cancelled.

Wire format, shared with `streamToChannel` in the JavaScript packages and
`stream_to_channel` in Python: payloads `{id, seq, delta, meta}` (meta on the
first event only), then `{id, seq, done: true, text?, error?}`; the final
event carries the whole text when it fits.

## 中文摘要

把 LLM 的輸出即時串流到 Hibira 頻道，React / Vue 的 `useStream()` 直接組回完整文字。`Stream` 實作 `io.Writer`：每收到一段文字就寫入，最後 `Close`（失敗時 `Fail`）。會自動批次（約每秒 10 個事件）、每個事件不超過方案的訊息大小，最後一個事件在放得下時附上全文，晚進來的使用者也能補齊。
