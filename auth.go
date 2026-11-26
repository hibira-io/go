package hibira

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidWebhook is returned by VerifyWebhook for a delivery whose key or
// signature does not match the app.
var ErrInvalidWebhook = errors.New("hibira: invalid webhook")

// Member is the user in a presence channel.
type Member struct {
	UserID string `json:"user_id"`
	// UserInfo is shown to the other members, such as {"name": "Ann"}.
	UserInfo any `json:"user_info,omitempty"`
}

// ChannelAuth is the JSON answer to a channel authorization request.
type ChannelAuth struct {
	Auth        string `json:"auth"`
	ChannelData string `json:"channel_data,omitempty"`
}

// User is the user of a connection's sign-in (pusher.signin()), which the
// Chat API and server-to-user events need.
type User struct {
	ID        string   `json:"id"`
	UserInfo  any      `json:"user_info,omitempty"`
	Watchlist []string `json:"watchlist,omitempty"`
}

// UserAuth is the JSON answer to a user sign-in request.
type UserAuth struct {
	Auth     string `json:"auth"`
	UserData string `json:"user_data"`
}

func hmacHex(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// AuthorizeChannel signs a private- or presence- channel subscription for
// socketID. Presence channels need member. It makes no network call.
func (c *Client) AuthorizeChannel(channel, socketID string, member *Member) (*ChannelAuth, error) {
	if c.Key == "" || c.Secret == "" {
		return nil, fmt.Errorf("%w: Key and Secret", ErrConfig)
	}
	if err := checkChannel(channel); err != nil {
		return nil, err
	}
	if err := checkSocketID(socketID); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(channel, "private-encrypted-"):
		return nil, errors.New("hibira: private-encrypted- channels are not supported")
	case strings.HasPrefix(channel, "presence-"):
		if member == nil || member.UserID == "" {
			return nil, errors.New("hibira: presence channels need a member with a UserID")
		}
		data, err := compact(member)
		if err != nil {
			return nil, err
		}
		return &ChannelAuth{
			Auth:        c.Key + ":" + hmacHex(c.Secret, socketID+":"+channel+":"+string(data)),
			ChannelData: string(data),
		}, nil
	case strings.HasPrefix(channel, "private-"):
		return &ChannelAuth{Auth: c.Key + ":" + hmacHex(c.Secret, socketID+":"+channel)}, nil
	}
	return nil, errors.New("hibira: only private- and presence- channels need authorization")
}

// AuthenticateUser signs a connection's user sign-in. It makes no network call.
func (c *Client) AuthenticateUser(socketID string, user User) (*UserAuth, error) {
	if c.Key == "" || c.Secret == "" {
		return nil, fmt.Errorf("%w: Key and Secret", ErrConfig)
	}
	if err := checkSocketID(socketID); err != nil {
		return nil, err
	}
	if user.ID == "" {
		return nil, errors.New("hibira: user needs an ID")
	}
	data, err := compact(user)
	if err != nil {
		return nil, err
	}
	return &UserAuth{
		Auth:     c.Key + ":" + hmacHex(c.Secret, socketID+"::user::"+string(data)),
		UserData: string(data),
	}, nil
}

// Webhook is a verified webhook delivery.
type Webhook struct {
	TimeMs int64          `json:"time_ms"`
	Events []WebhookEvent `json:"events"`
	// Raw is the whole body, for fields this struct does not name.
	Raw json.RawMessage `json:"-"`
}

// WebhookEvent is one event of a webhook: channel_occupied,
// channel_vacated, member_added, member_removed, client_event,
// subscription_count, cache_miss, chat_message and others.
type WebhookEvent struct {
	Name     string `json:"name"`
	Channel  string `json:"channel,omitempty"`
	Event    string `json:"event,omitempty"`
	Data     string `json:"data,omitempty"`
	SocketID string `json:"socket_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	// Raw is the whole event.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the raw event next to the named fields.
func (e *WebhookEvent) UnmarshalJSON(b []byte) error {
	type plain WebhookEvent
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = WebhookEvent(p)
	e.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// VerifyWebhook checks a webhook's X-Pusher-Key and X-Pusher-Signature
// headers against the raw body and returns the parsed delivery. It returns
// an error wrapping ErrInvalidWebhook when they do not match.
func (c *Client) VerifyWebhook(key, signature string, body []byte) (*Webhook, error) {
	if c.Key == "" || c.Secret == "" {
		return nil, fmt.Errorf("%w: Key and Secret", ErrConfig)
	}
	if key == "" || !hmac.Equal([]byte(key), []byte(c.Key)) {
		return nil, fmt.Errorf("%w: key does not match this app", ErrInvalidWebhook)
	}
	got, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(signature)))
	m := hmac.New(sha256.New, []byte(c.Secret))
	m.Write(body)
	if err != nil || !hmac.Equal(got, m.Sum(nil)) {
		return nil, fmt.Errorf("%w: signature does not match", ErrInvalidWebhook)
	}
	var w Webhook
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("%w: body is not JSON", ErrInvalidWebhook)
	}
	w.Raw = append(json.RawMessage(nil), body...)
	return &w, nil
}
