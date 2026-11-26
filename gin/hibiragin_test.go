package hibiragin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/waitingroom"
)

// Pass from the shared waiting room test vector (hibira-io/waiting-room).
const (
	wrSecret = "wr-secret-0123456789"
	wrPass   = "eyJ2IjoxLCJhcHAiOiJ3ci1hcHAiLCJyb29tIjoicHJlc2FsZSIsIm4iOjQyLCJ0aWQiOiJkNzlmZTc4OWEzOGI4NjMxYjUiLCJpYXQiOjE4MDAwMDAwMDAsImV4cCI6MTgwMDAwMTgwMH0.q7Uc_O6XKwNA_LVLQITlgv_YtDi9dIwiRzUUUSsGa08"
)

var client = &hibira.Client{AppID: "1", Key: "key", Secret: "secret"}

func sign(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

func router() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id := c.GetHeader("X-User"); id != "" {
			c.Set("user", id)
		}
	})
	r.POST("/auth", Auth(client, func(c *gin.Context, ch string) (bool, *hibira.Member) {
		id := c.GetString("user")
		if id == "" {
			return false, nil
		}
		if ch == "private-user-"+id {
			return true, nil
		}
		return ch == "presence-room-7", &hibira.Member{UserID: id}
	}))
	r.POST("/user-auth", UserAuth(client, func(c *gin.Context) *hibira.User {
		if id := c.GetString("user"); id != "" {
			return &hibira.User{ID: id}
		}
		return nil
	}))
	r.POST("/webhook", Webhook(client, func(c *gin.Context, h *hibira.Webhook) error {
		if h.Events[0].Name == "fail" {
			return errors.New("boom")
		}
		return nil
	}))
	r.POST("/approval", ApprovalCallback("secret", func(c *gin.Context, e *approvals.Event) error { return nil }))
	room := waitingroom.Guard{AppKey: "key", Secret: []string{wrSecret}, Room: "presale", Now: func() time.Time { return time.Unix(1800000100, 0) }}
	r.GET("/drop", WaitingRoom(room), func(c *gin.Context) {
		c.String(200, "ticket %d", Pass(c).Number)
	})
	r.POST("/drop", WaitingRoom(room), func(c *gin.Context) { c.String(200, "ok") })
	return r
}

func do(r http.Handler, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuth(t *testing.T) {
	r := router()
	w := do(r, "POST", "/auth", "socket_id=1.2&channel_name=private-user-42", map[string]string{"X-User": "42"})
	var a hibira.ChannelAuth
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 200 || a.Auth != "key:"+sign("secret", "1.2:private-user-42") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = do(r, "POST", "/auth", `{"socket_id":"1.2","channel_name":"presence-room-7"}`, map[string]string{"X-User": "42", "Content-Type": "application/json"})
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 200 || a.ChannelData != `{"user_id":"42"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do(r, "POST", "/auth", "socket_id=1.2&channel_name=private-user-7", map[string]string{"X-User": "42"}); w.Code != 403 {
		t.Fatalf("other user's channel: %d", w.Code)
	}
	if w := do(r, "POST", "/auth", "socket_id=1.2&channel_name=private-user-42", nil); w.Code != 403 {
		t.Fatalf("signed out: %d", w.Code)
	}
	if w := do(r, "POST", "/auth", "channel_name=private-user-42", nil); w.Code != 400 {
		t.Fatalf("no socket: %d", w.Code)
	}
}

func TestUserAuth(t *testing.T) {
	r := router()
	w := do(r, "POST", "/user-auth", "socket_id=1.2", map[string]string{"X-User": "42"})
	var a hibira.UserAuth
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 200 || a.UserData != `{"id":"42"}` || a.Auth != "key:"+sign("secret", `1.2::user::{"id":"42"}`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do(r, "POST", "/user-auth", "socket_id=1.2", nil); w.Code != 403 {
		t.Fatalf("%d", w.Code)
	}
}

func TestWebhook(t *testing.T) {
	r := router()
	body := `{"time_ms":1,"events":[{"name":"channel_occupied","channel":"a"}]}`
	h := map[string]string{"X-Pusher-Key": "key", "X-Pusher-Signature": sign("secret", body)}
	if w := do(r, "POST", "/webhook", body, h); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	h["X-Pusher-Signature"] = sign("other", body)
	if w := do(r, "POST", "/webhook", body, h); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
	fail := `{"time_ms":1,"events":[{"name":"fail"}]}`
	if w := do(r, "POST", "/webhook", fail, map[string]string{"X-Pusher-Key": "key", "X-Pusher-Signature": sign("secret", fail)}); w.Code != 500 {
		t.Fatalf("%d", w.Code)
	}
}

func TestApprovalCallback(t *testing.T) {
	r := router()
	body := `{"id":"evt_1","type":"approval.decided","data":{"approval":{"approval_id":"ap_1","status":"approved"}}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if w := do(r, "POST", "/approval", body, map[string]string{"Hibira-Signature": "t=" + ts + ",v1=" + sign("secret", ts+"."+body)}); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do(r, "POST", "/approval", body, map[string]string{"Hibira-Signature": "t=" + ts + ",v1=00"}); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestWaitingRoom(t *testing.T) {
	r := router()
	w := do(r, "GET", "/drop", "", nil)
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "https://ws.hibira.io/wait/key/presale?return=") {
		t.Fatalf("no pass: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = do(r, "GET", "/drop?hibira_pass="+wrPass, "", nil)
	if w.Code != 302 || w.Header().Get("Location") != "http://example.com/drop" || !strings.Contains(w.Header().Get("Set-Cookie"), "hibira_wr_presale="+wrPass) {
		t.Fatalf("back from queue: %d %s %s", w.Code, w.Header().Get("Location"), w.Header().Get("Set-Cookie"))
	}
	w = do(r, "GET", "/drop", "", map[string]string{"Cookie": "hibira_wr_presale=" + wrPass})
	if w.Code != 200 || w.Body.String() != "ticket 42" {
		t.Fatalf("with pass: %d %s", w.Code, w.Body)
	}
	if w := do(r, "POST", "/drop", "", nil); w.Code != 403 {
		t.Fatalf("POST without pass: %d", w.Code)
	}
}
