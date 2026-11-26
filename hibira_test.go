package hibira

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	testKey    = "278d425bdf160c739803"
	testSecret = "7ad3773142a6692b25b8"
)

func testClient(host string) *Client {
	return &Client{AppID: "3", Key: testKey, Secret: testSecret, Host: host, Now: func() time.Time { return time.Unix(1353088179, 0) }}
}

// Pusher's documented test vector for private channels.
func TestAuthorizePrivateChannel(t *testing.T) {
	a, err := testClient("").AuthorizeChannel("private-foobar", "1234.1234", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := testKey + ":58df8b0c36d6982b82c3ecf6b4662e34fe8c25bba48f5369f135bf843651c3a4"
	if a.Auth != want || a.ChannelData != "" {
		t.Fatalf("got %+v, want %s", a, want)
	}
}

func TestAuthorizePresenceChannel(t *testing.T) {
	c := testClient("")
	a, err := c.AuthorizeChannel("presence-room-7", "1234.1234", &Member{UserID: "42", UserInfo: map[string]string{"name": "Ann <&>"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ChannelData != `{"user_id":"42","user_info":{"name":"Ann <&>"}}` {
		t.Fatalf("channel_data %s", a.ChannelData)
	}
	if a.Auth != testKey+":"+hmacHex(testSecret, "1234.1234:presence-room-7:"+a.ChannelData) {
		t.Fatalf("auth %s", a.Auth)
	}
	if _, err := c.AuthorizeChannel("presence-room-7", "1234.1234", nil); err == nil {
		t.Fatal("presence without member")
	}
	for _, bad := range [][2]string{{"public", "1.1"}, {"private-encrypted-x", "1.1"}, {"private-x", "abc"}, {"private-x#y", "1.1"}} {
		if _, err := c.AuthorizeChannel(bad[0], bad[1], nil); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestAuthenticateUser(t *testing.T) {
	a, err := testClient("").AuthenticateUser("1234.1234", User{ID: "42", UserInfo: map[string]string{"name": "Ann"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.UserData != `{"id":"42","user_info":{"name":"Ann"}}` {
		t.Fatalf("user_data %s", a.UserData)
	}
	if a.Auth != testKey+":"+hmacHex(testSecret, "1234.1234::user::"+a.UserData) {
		t.Fatalf("auth %s", a.Auth)
	}
	if _, err := testClient("").AuthenticateUser("1234.1234", User{}); err == nil {
		t.Fatal("user without ID")
	}
}

func TestVerifyWebhook(t *testing.T) {
	c := testClient("")
	body := []byte(`{"time_ms":1700000000000,"events":[{"name":"member_added","channel":"presence-room-7","user_id":"42"}]}`)
	sig := hmacHex(testSecret, string(body))
	w, err := c.VerifyWebhook(testKey, strings.ToUpper(sig), body)
	if err != nil {
		t.Fatal(err)
	}
	if w.TimeMs != 1700000000000 || len(w.Events) != 1 || w.Events[0].UserID != "42" || len(w.Events[0].Raw) == 0 {
		t.Fatalf("%+v", w)
	}
	if _, err := c.VerifyWebhook(testKey, sig, append(body, ' ')); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("tampered body: %v", err)
	}
	if _, err := c.VerifyWebhook("other", sig, body); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("wrong key: %v", err)
	}
}

// checkSigned verifies a request the way the server does.
func checkSigned(t *testing.T, r *http.Request, body []byte) {
	t.Helper()
	q := r.URL.Query()
	sig := q.Get("auth_signature")
	keys := []string{}
	for k := range q {
		if k != "auth_signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + q.Get(k)
	}
	path, _ := url.PathUnescape(r.URL.EscapedPath())
	if want := hmacHex(testSecret, r.Method+"\n"+path+"\n"+strings.Join(pairs, "&")); sig != want {
		t.Fatalf("bad signature for %s %s", r.Method, path)
	}
	if q.Get("auth_key") != testKey {
		t.Fatal("auth_key")
	}
	if len(body) > 0 && q.Get("body_md5") == "" {
		t.Fatal("body_md5 missing")
	}
}

type recorded struct {
	method, path string
	query        url.Values
	body         map[string]any
}

func server(t *testing.T, status int, answer string, got *recorded) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.URL.Path, "/apps/") {
			checkSigned(t, r, b)
		}
		got.method, got.path, got.query, got.body = r.Method, r.URL.Path, r.URL.Query(), nil
		if len(b) > 0 {
			_ = json.Unmarshal(b, &got.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTrigger(t *testing.T) {
	var got recorded
	c := testClient(server(t, 200, `{}`, &got).URL)
	ctx := context.Background()
	if err := c.Trigger(ctx, "orders", "created", map[string]any{"id": 1042}); err != nil {
		t.Fatal(err)
	}
	if got.path != "/apps/3/events" || got.body["name"] != "created" || got.body["data"] != `{"id":1042}` {
		t.Fatalf("%+v", got)
	}
	if err := c.Publish(ctx, Event{Channels: []string{"a", "b"}, Name: "e", Data: "raw", SocketID: "1.2"}); err != nil {
		t.Fatal(err)
	}
	if got.body["data"] != "raw" || got.body["socket_id"] != "1.2" || len(got.body["channels"].([]any)) != 2 {
		t.Fatalf("%+v", got.body)
	}
	if err := c.TriggerBatch(ctx, []BatchEvent{{Channel: "a", Name: "e", Data: 1}}); err != nil {
		t.Fatal(err)
	}
	if got.path != "/apps/3/batch_events" {
		t.Fatal(got.path)
	}
	if err := c.Publish(ctx, Event{Name: "e"}); err == nil {
		t.Fatal("no channels")
	}
}

func TestErrors(t *testing.T) {
	var got recorded
	c := testClient(server(t, 413, `{"error":"payload too large"}`, &got).URL)
	err := c.Trigger(context.Background(), "a", "e", "x")
	var e *Error
	if !errors.As(err, &e) || e.Status != 413 || e.Message != "payload too large" || StatusOf(err) != 413 {
		t.Fatalf("%v", err)
	}
	if err := (&Client{}).Trigger(context.Background(), "a", "e", "x"); !errors.Is(err, ErrConfig) {
		t.Fatalf("%v", err)
	}
}

func TestChannels(t *testing.T) {
	var got recorded
	c := testClient(server(t, 200, `{"channels":{"presence-room-7":{"user_count":3}}}`, &got).URL)
	chs, err := c.Channels(context.Background(), "presence-", "user_count")
	if err != nil {
		t.Fatal(err)
	}
	if chs["presence-room-7"].UserCount != 3 || got.query.Get("filter_by_prefix") != "presence-" || got.query.Get("info") != "user_count" {
		t.Fatalf("%+v %+v", chs, got)
	}
	c = testClient(server(t, 200, `{"users":[{"id":"1"},{"id":"2"}]}`, &got).URL)
	users, err := c.Users(context.Background(), "presence-room-7")
	if err != nil || len(users) != 2 || got.path != "/apps/3/channels/presence-room-7/users" {
		t.Fatalf("%v %v %s", users, err, got.path)
	}
}

func TestChat(t *testing.T) {
	var got recorded
	ctx := context.Background()
	c := testClient(server(t, 201, `{"conversation":{"id":"order 1042","members":[{"user_id":"ann","read_seq":0}],"last_seq":0}}`, &got).URL)
	conv, err := c.PutConversation(ctx, "order 1042", ConversationInput{Name: "Order", Members: []string{"ann", "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	if conv.ID != "order 1042" || got.method != "PUT" || got.path != "/apps/3/chat/conversations/order 1042" || got.body["name"] != "Order" {
		t.Fatalf("%+v %+v", conv, got)
	}
	c = testClient(server(t, 201, `{"message":{"id":"m1","seq":4,"sender":"bob","kind":"text","text":"hi"},"push":{"sent":1}}`, &got).URL)
	sent, err := c.SendMessage(ctx, "order-1042", MessageInput{UserID: "bob", Text: "hi", Push: "offline"})
	if err != nil || sent.Message.Seq != 4 || got.body["user_id"] != "bob" {
		t.Fatalf("%+v %v %+v", sent, err, got)
	}
	if _, err := c.SendMessage(ctx, "order-1042", MessageInput{Text: "hi"}); err == nil {
		t.Fatal("no user")
	}
	c = testClient(server(t, 200, `{"messages":[{"seq":5,"kind":"text","text":"a"}],"last_seq":5}`, &got).URL)
	page, err := c.ListMessages(ctx, "order-1042", ListOptions{After: 4, Limit: 50})
	if err != nil || page.LastSeq != 5 || got.query.Get("after") != "4" || got.query.Get("limit") != "50" {
		t.Fatalf("%+v %v %+v", page, err, got)
	}
}

func TestPush(t *testing.T) {
	var got recorded
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		got.path = r.URL.Path
		_ = json.Unmarshal(b, &got.body)
		_, _ = io.WriteString(w, `{"publishId":"pub-1"}`)
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	if _, err := c.PushToUsers(context.Background(), []string{"42"}, nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("%v", err)
	}
	c.PushInstanceID, c.PushSecretKey = "inst", "push-secret"
	res, err := c.PushToInterests(context.Background(), []string{"news"}, map[string]any{"web": map[string]any{"notification": map[string]any{"title": "x"}}})
	if err != nil || res.PublishID != "pub-1" || auth != "Bearer push-secret" || got.path != "/publish_api/v1/instances/inst/publishes/interests" {
		t.Fatalf("%+v %v %s %s", res, err, auth, got.path)
	}
	if got.body["interests"].([]any)[0] != "news" || got.body["web"] == nil {
		t.Fatalf("%+v", got.body)
	}

	tok, err := c.PushToken("42", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	m := hmac.New(sha256.New, []byte("push-secret"))
	m.Write([]byte(parts[0] + "." + parts[1]))
	if len(parts) != 3 || parts[2] != base64.RawURLEncoding.EncodeToString(m.Sum(nil)) {
		t.Fatal("bad token signature")
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if string(claims) != `{"sub":"42","iss":"https://inst.pushnotifications.pusher.com","iat":1353088179,"exp":1353091779}` {
		t.Fatalf("claims %s", claims)
	}
}

func TestAuthHandler(t *testing.T) {
	c := testClient("")
	h := c.AuthHandler(func(r *http.Request, ch string) (bool, *Member) {
		if ch == "private-user-42" {
			return true, nil
		}
		if ch == "presence-room-7" {
			return true, &Member{UserID: "42"}
		}
		return false, nil
	})
	do := func(ct, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/hibira/auth", strings.NewReader(body))
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := do("application/x-www-form-urlencoded", "socket_id=1234.1234&channel_name=private-user-42")
	var a ChannelAuth
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 200 || !strings.HasPrefix(a.Auth, testKey+":") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = do("application/json", `{"socket_id":"1234.1234","channel_name":"presence-room-7"}`)
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if w.Code != 200 || a.ChannelData != `{"user_id":"42"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do("application/x-www-form-urlencoded", "socket_id=1234.1234&channel_name=private-user-7"); w.Code != 403 {
		t.Fatalf("%d", w.Code)
	}
	if w := do("application/x-www-form-urlencoded", "channel_name=private-user-42"); w.Code != 400 {
		t.Fatalf("%d", w.Code)
	}
}

func TestUserAuthAndWebhookHandlers(t *testing.T) {
	c := testClient("")
	uh := c.UserAuthHandler(func(r *http.Request) *User {
		if r.Header.Get("X-User") == "" {
			return nil
		}
		return &User{ID: r.Header.Get("X-User")}
	})
	r := httptest.NewRequest("POST", "/hibira/user-auth", strings.NewReader("socket_id=1.2"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-User", "42")
	w := httptest.NewRecorder()
	uh.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"user_data":"{\"id\":\"42\"}"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	var seen *Webhook
	wh := c.WebhookHandler(func(r *http.Request, hook *Webhook) error { seen = hook; return nil })
	body := `{"time_ms":1,"events":[{"name":"channel_occupied","channel":"a"}]}`
	r = httptest.NewRequest("POST", "/hibira/webhook", strings.NewReader(body))
	r.Header.Set("X-Pusher-Key", testKey)
	sum := hmac.New(sha256.New, []byte(testSecret))
	sum.Write([]byte(body))
	r.Header.Set("X-Pusher-Signature", hex.EncodeToString(sum.Sum(nil)))
	w = httptest.NewRecorder()
	wh.ServeHTTP(w, r)
	if w.Code != 200 || seen == nil || seen.Events[0].Name != "channel_occupied" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest("POST", "/hibira/webhook", strings.NewReader(body))
	r.Header.Set("X-Pusher-Key", testKey)
	r.Header.Set("X-Pusher-Signature", "00")
	w = httptest.NewRecorder()
	wh.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("HIBIRA_APP_ID", "1")
	t.Setenv("HIBIRA_APP_KEY", "k")
	t.Setenv("HIBIRA_APP_SECRET", "s")
	t.Setenv("HIBIRA_HOST", "localhost")
	t.Setenv("HIBIRA_PORT", "6001")
	t.Setenv("HIBIRA_USE_TLS", "false")
	c := FromEnv()
	if c.baseURL() != "http://localhost:6001" || c.AppID != "1" {
		t.Fatalf("%+v %s", c, c.baseURL())
	}
	if c.Approvals().Host != "http://localhost:6001" {
		t.Fatal("approvals host")
	}
}
