package hibira

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PushResult is the answer to a push publish.
type PushResult struct {
	PublishID string `json:"publishId"`
}

func (c *Client) pushSettings() (string, string, error) {
	if c.PushInstanceID == "" || c.PushSecretKey == "" {
		return "", "", fmt.Errorf("%w: PushInstanceID and PushSecretKey (HIBIRA_PUSH_INSTANCE_ID, HIBIRA_PUSH_SECRET_KEY)", ErrConfig)
	}
	return c.PushInstanceID, c.PushSecretKey, nil
}

func (c *Client) push(ctx context.Context, method, path string, body any, out any) error {
	instance, secret, err := c.pushSettings()
	if err != nil {
		return err
	}
	var raw []byte
	if body != nil {
		if raw, err = compact(body); err != nil {
			return err
		}
	}
	base := strings.TrimRight(c.PushEndpoint, "/")
	if base == "" {
		base = c.baseURL()
	}
	u := base + strings.Replace(path, "{instance}", url.PathEscape(instance), 1)
	return c.send(ctx, method, u, raw, http.Header{"Authorization": {"Bearer " + secret}}, out)
}

func withField(body map[string]any, k string, v any) map[string]any {
	out := make(map[string]any, len(body)+1)
	for key, val := range body {
		out[key] = val
	}
	out[k] = v
	return out
}

// PushToInterests sends a push notification to the devices subscribed to
// any of interests. body is a Beams publish body:
//
//	map[string]any{
//		"web":  map[string]any{"notification": map[string]any{"title": "New post", "deep_link": "https://example.com/p/1"}},
//		"fcm":  map[string]any{"notification": map[string]any{"title": "New post"}},
//		"apns": map[string]any{"aps": map[string]any{"alert": map[string]any{"title": "New post"}}},
//	}
func (c *Client) PushToInterests(ctx context.Context, interests []string, body map[string]any) (*PushResult, error) {
	if len(interests) == 0 {
		return nil, errors.New("hibira: give at least one interest")
	}
	var out PushResult
	err := c.push(ctx, "POST", "/publish_api/v1/instances/{instance}/publishes/interests", withField(body, "interests", interests), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// PushToUsers sends a push notification to the devices of up to 1000 users.
func (c *Client) PushToUsers(ctx context.Context, userIDs []string, body map[string]any) (*PushResult, error) {
	if len(userIDs) == 0 {
		return nil, errors.New("hibira: give at least one user ID")
	}
	var out PushResult
	err := c.push(ctx, "POST", "/publish_api/v1/instances/{instance}/publishes/users", withField(body, "users", userIDs), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePushUser removes a user and their devices from push.
func (c *Client) DeletePushUser(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("hibira: userID is required")
	}
	return c.push(ctx, "DELETE", "/customer_api/v1/instances/{instance}/users/"+url.PathEscape(userID), nil, nil)
}

// PushToken is a push user token for setUserId in the web, Android and iOS
// SDKs. ttl defaults to 24 hours. It makes no network call.
func (c *Client) PushToken(userID string, ttl time.Duration) (string, error) {
	instance, secret, err := c.pushSettings()
	if err != nil {
		return "", err
	}
	if userID == "" || len(userID) > 164 {
		return "", errors.New("hibira: userID must be 1 to 164 characters")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := c.now().Unix()
	b64 := base64.RawURLEncoding.EncodeToString
	header, _ := compact(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, err := compact(struct {
		Sub string `json:"sub"`
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}{userID, "https://" + instance + ".pushnotifications.pusher.com", now, now + int64(ttl/time.Second)})
	if err != nil {
		return "", err
	}
	unsigned := b64(header) + "." + b64(claims)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(unsigned))
	return unsigned + "." + b64(m.Sum(nil)), nil
}
