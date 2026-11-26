// Package hibira is the Go server library for Hibira (https://hibira.io),
// hosted realtime that speaks the Pusher Channels protocol: publish events,
// authorize private and presence channels, sign users in, verify webhooks,
// send push notifications and use the Chat API. pusher-js, Laravel Echo and
// the @hibira-io/* front-end packages connect to Hibira unchanged.
//
//	client := hibira.FromEnv() // HIBIRA_APP_ID, HIBIRA_APP_KEY, HIBIRA_APP_SECRET
//	err := client.Trigger(ctx, "orders", "created", map[string]any{"id": 1042})
//
// The net/http handlers (AuthHandler, UserAuthHandler, WebhookHandler) work
// with the standard library, chi, gorilla/mux and anything else that takes an
// http.Handler. Gin, Echo and Fiber have their own modules under
// github.com/hibira-io/go/gin, /echo and /fiber.
package hibira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hibira-io/go/approvals"
)

// DefaultHost serves the Hibira HTTP API.
const DefaultHost = "https://ws.hibira.io"

// DefaultTimeout is how long one API call may take unless Client.Timeout says otherwise.
const DefaultTimeout = 10 * time.Second

const userAgent = "hibira-go"

// ErrConfig is returned when a setting the call needs is missing.
var ErrConfig = errors.New("hibira: missing setting")

// Error is an answer from the Hibira API other than 2xx.
type Error struct {
	Status  int
	Message string
	Body    []byte
}

func (e *Error) Error() string { return fmt.Sprintf("hibira: %d %s", e.Status, e.Message) }

// StatusOf returns the HTTP status of an *Error in err's chain, or 0.
// 401/403: bad credentials; 404: unknown app, channel or conversation;
// 413: the event is over the plan's message size; 429: rate limited.
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// Client calls the Hibira HTTP API for one app. The zero value is not usable:
// set AppID, Key and Secret, or use FromEnv.
type Client struct {
	AppID  string
	Key    string
	Secret string
	// Host is the API base URL, such as https://ws.hibira.io (the default) or
	// the host shown on your app page. A bare host name means https.
	Host string

	// PushInstanceID and PushSecretKey turn on the push methods.
	PushInstanceID string
	PushSecretKey  string
	// PushEndpoint defaults to Host.
	PushEndpoint string

	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Timeout of each call, default DefaultTimeout.
	Timeout time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
}

// FromEnv reads the settings from the environment: HIBIRA_APP_ID,
// HIBIRA_APP_KEY, HIBIRA_APP_SECRET, HIBIRA_HOST (with HIBIRA_PORT and
// HIBIRA_USE_TLS), HIBIRA_PUSH_INSTANCE_ID, HIBIRA_PUSH_SECRET_KEY and
// HIBIRA_PUSH_ENDPOINT.
func FromEnv() *Client {
	c := &Client{
		AppID:          os.Getenv("HIBIRA_APP_ID"),
		Key:            os.Getenv("HIBIRA_APP_KEY"),
		Secret:         os.Getenv("HIBIRA_APP_SECRET"),
		PushInstanceID: os.Getenv("HIBIRA_PUSH_INSTANCE_ID"),
		PushSecretKey:  os.Getenv("HIBIRA_PUSH_SECRET_KEY"),
		PushEndpoint:   os.Getenv("HIBIRA_PUSH_ENDPOINT"),
	}
	if h := os.Getenv("HIBIRA_HOST"); h != "" {
		if !strings.Contains(h, "://") {
			scheme := "https"
			if v := strings.ToLower(strings.TrimSpace(os.Getenv("HIBIRA_USE_TLS"))); v == "0" || v == "false" || v == "no" || v == "off" {
				scheme = "http"
			}
			h = scheme + "://" + h
			if p := os.Getenv("HIBIRA_PORT"); p != "" && !(scheme == "https" && p == "443") && !(scheme == "http" && p == "80") {
				h += ":" + p
			}
		}
		c.Host = h
	}
	return c
}

// Approvals returns an Approvals client for the same app; see package approvals.
func (c *Client) Approvals() *approvals.Client {
	return &approvals.Client{AppID: c.AppID, Key: c.Key, Secret: c.Secret, Host: c.baseURL(), HTTPClient: c.HTTPClient, Now: c.Now}
}

func (c *Client) baseURL() string {
	h := strings.TrimRight(c.Host, "/")
	if h == "" {
		return DefaultHost
	}
	if !strings.Contains(h, "://") {
		h = "https://" + h
	}
	return h
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

func (c *Client) checkKeys() error {
	if c.AppID == "" || c.Key == "" || c.Secret == "" {
		return fmt.Errorf("%w: AppID, Key and Secret (HIBIRA_APP_ID, HIBIRA_APP_KEY, HIBIRA_APP_SECRET)", ErrConfig)
	}
	return nil
}

// compact is JSON without HTML escaping or a trailing newline, the form
// signatures are computed over.
func compact(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// call sends a signed request to /apps/{app_id}{resource} and decodes the
// answer into out (when not nil).
func (c *Client) call(ctx context.Context, method, resource string, query url.Values, body any, out any) error {
	if err := c.checkKeys(); err != nil {
		return err
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = compact(body); err != nil {
			return err
		}
	}
	path := "/apps/" + url.PathEscape(c.AppID) + resource
	signPath, err := url.PathUnescape(path)
	if err != nil {
		return err
	}
	u := c.baseURL() + path + "?" + approvals.SignedQuery(c.Key, c.Secret, method, signPath, query, raw, c.now())
	return c.send(ctx, method, u, raw, nil, out)
}

func (c *Client) send(ctx context.Context, method, u string, body []byte, header http.Header, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		msg := strings.TrimSpace(string(data))
		var e struct {
			Error       string `json:"error"`
			Message     string `json:"message"`
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &e) == nil {
			for _, s := range []string{e.Error, e.Message, e.Description} {
				if s != "" {
					msg = s
					break
				}
			}
		}
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return &Error{Status: res.StatusCode, Message: msg, Body: data}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("hibira: bad answer from %s: %w", method, err)
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }
