package hibira

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
)

// MaxWebhookBody is the largest webhook body the handlers read.
const MaxWebhookBody = 1 << 20

// AuthorizeFunc decides whether the user behind r may subscribe to channel.
// For presence- channels it also returns the member to show; for private-
// channels the member is ignored and may be nil.
type AuthorizeFunc func(r *http.Request, channel string) (ok bool, member *Member)

// AuthRequest is what pusher-js sends to the channel authorization and user
// sign-in endpoints.
type AuthRequest struct {
	SocketID string `json:"socket_id"`
	Channel  string `json:"channel_name"`
}

// ParseAuthRequest reads socket_id and channel_name from a request body
// with the given Content-Type: form-encoded (the pusher-js default) or JSON.
func ParseAuthRequest(contentType string, body []byte) (AuthRequest, error) {
	var a AuthRequest
	ct, _, _ := mime.ParseMediaType(contentType)
	if ct == "application/json" {
		err := json.Unmarshal(body, &a)
		return a, err
	}
	q, err := url.ParseQuery(string(body))
	if err != nil {
		return a, err
	}
	return AuthRequest{SocketID: q.Get("socket_id"), Channel: q.Get("channel_name")}, nil
}

// AuthAnswer is the status and JSON body that answer a channel
// authorization request, given the app's decision. Framework adapters use it.
func (c *Client) AuthAnswer(a AuthRequest, ok bool, member *Member) (int, any) {
	if a.SocketID == "" || a.Channel == "" {
		return http.StatusBadRequest, errorBody("socket_id and channel_name are required")
	}
	if !ok {
		return http.StatusForbidden, errorBody("forbidden")
	}
	auth, err := c.AuthorizeChannel(a.Channel, a.SocketID, member)
	if err != nil {
		return errorStatus(err), errorBody(err.Error())
	}
	return http.StatusOK, auth
}

// UserAuthAnswer is the status and JSON body that answer a user sign-in
// request for user (nil refuses it). Framework adapters use it.
func (c *Client) UserAuthAnswer(a AuthRequest, user *User) (int, any) {
	if a.SocketID == "" {
		return http.StatusBadRequest, errorBody("socket_id is required")
	}
	if user == nil {
		return http.StatusForbidden, errorBody("forbidden")
	}
	auth, err := c.AuthenticateUser(a.SocketID, *user)
	if err != nil {
		return errorStatus(err), errorBody(err.Error())
	}
	return http.StatusOK, auth
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

func errorStatus(err error) int {
	if errors.Is(err, ErrConfig) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// ReadAuthRequest reads socket_id and channel_name from a form-encoded (the
// pusher-js default) or JSON request body.
func ReadAuthRequest(r *http.Request) (AuthRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return AuthRequest{}, err
	}
	return ParseAuthRequest(r.Header.Get("Content-Type"), body)
}

// WriteJSON writes v as a JSON answer with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := compact(v)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"error":"encode"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// AuthHandler answers pusher-js channel authorization requests
// (channelAuthorization.endpoint) for private- and presence- channels.
// fn decides who may subscribe; a refusal answers 403.
//
//	http.Handle("POST /hibira/auth", client.AuthHandler(func(r *http.Request, ch string) (bool, *hibira.Member) {
//		u := currentUser(r)
//		if u == nil { return false, nil }
//		if ch == "private-user-"+u.ID { return true, nil }
//		if strings.HasPrefix(ch, "presence-room-") { return true, &hibira.Member{UserID: u.ID, UserInfo: map[string]string{"name": u.Name}} }
//		return false, nil
//	}))
func (c *Client) AuthHandler(fn AuthorizeFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		a, err := ReadAuthRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "socket_id and channel_name are required")
			return
		}
		if a.SocketID == "" || a.Channel == "" {
			writeError(w, http.StatusBadRequest, "socket_id and channel_name are required")
			return
		}
		ok, member := fn(r, a.Channel)
		status, body := c.AuthAnswer(a, ok, member)
		WriteJSON(w, status, body)
	})
}

// UserAuthHandler answers pusher-js user sign-in requests
// (userAuthentication.endpoint). fn returns the signed-in user, or nil to
// answer 403.
func (c *Client) UserAuthHandler(fn func(r *http.Request) *User) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		a, err := ReadAuthRequest(r)
		if err != nil || a.SocketID == "" {
			writeError(w, http.StatusBadRequest, "socket_id is required")
			return
		}
		status, body := c.UserAuthAnswer(a, fn(r))
		WriteJSON(w, status, body)
	})
}

// WebhookHandler verifies webhook deliveries and passes them to fn. A
// forged delivery gets 401; an error from fn answers 500 so Hibira retries.
func (c *Client) WebhookHandler(fn func(r *http.Request, hook *Webhook) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBody+1))
		if err != nil || len(body) > MaxWebhookBody {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		hook, err := c.VerifyWebhook(r.Header.Get("X-Pusher-Key"), r.Header.Get("X-Pusher-Signature"), body)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid webhook")
			return
		}
		if err := fn(r, hook); err != nil {
			writeError(w, http.StatusInternalServerError, "webhook handler failed")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{})
	})
}
