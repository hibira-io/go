// Package hibiraecho connects Hibira to Echo (v5): channel authorization and
// user sign-in endpoints for pusher-js, webhook and Approvals callback
// receivers, and a waiting room middleware. Your functions get the
// *echo.Context, so whatever your auth middleware stored there is at hand.
//
//	client := hibira.FromEnv()
//	e := echo.New()
//	g := e.Group("/hibira", requireLogin)
//	g.POST("/auth", hibiraecho.Auth(client, func(c *echo.Context, channel string) (bool, *hibira.Member) {
//		u := c.Get("user").(*User)
//		if channel == "private-user-"+u.ID {
//			return true, nil
//		}
//		return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: map[string]string{"name": u.Name}}
//	}))
//	g.POST("/user-auth", hibiraecho.UserAuth(client, func(c *echo.Context) *hibira.User {
//		return &hibira.User{ID: c.Get("user").(*User).ID}
//	}))
//	e.POST("/hibira/webhook", hibiraecho.Webhook(client, onWebhook))
//	e.GET("/drop", dropPage, hibiraecho.WaitingRoom(room))
//
// On Echo v4, mount the net/http handlers of github.com/hibira-io/go with
// echo.WrapHandler and waitingroom.Guard.Middleware with echo.WrapMiddleware.
package hibiraecho

import (
	"io"
	"net/http"
	"time"

	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/waitingroom"
	"github.com/labstack/echo/v5"
)

// PassKey is the echo.Context key WaitingRoom stores the visitor's pass under.
const PassKey = "hibira.pass"

const maxBody = 1 << 20

func errJSON(c *echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

func readAuth(c *echo.Context) (hibira.AuthRequest, error) {
	r := c.Request()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return hibira.AuthRequest{}, err
	}
	return hibira.ParseAuthRequest(r.Header.Get("Content-Type"), body)
}

// Auth answers pusher-js channel authorization requests. fn decides whether
// the request's user may subscribe to channel, and for presence- channels
// returns the member to show.
func Auth(client *hibira.Client, fn func(c *echo.Context, channel string) (bool, *hibira.Member)) echo.HandlerFunc {
	return func(c *echo.Context) error {
		a, err := readAuth(c)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "bad request")
		}
		allowed, member := false, (*hibira.Member)(nil)
		if a.SocketID != "" && a.Channel != "" {
			allowed, member = fn(c, a.Channel)
		}
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(client.AuthAnswer(a, allowed, member))
	}
}

// UserAuth answers pusher-js user sign-in requests (pusher.signin()), which
// the Chat API and server-to-user events need. fn returns the signed-in user,
// or nil to refuse.
func UserAuth(client *hibira.Client, fn func(c *echo.Context) *hibira.User) echo.HandlerFunc {
	return func(c *echo.Context) error {
		a, err := readAuth(c)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "bad request")
		}
		var u *hibira.User
		if a.SocketID != "" {
			u = fn(c)
		}
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(client.UserAuthAnswer(a, u))
	}
}

func readBody(c *echo.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxBody+1))
	return body, err == nil && len(body) <= maxBody
}

// Webhook verifies Hibira webhook deliveries and passes them to fn. A forged
// delivery gets 401; an error from fn answers 500 so Hibira retries.
func Webhook(client *hibira.Client, fn func(c *echo.Context, hook *hibira.Webhook) error) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, ok := readBody(c)
		if !ok {
			return errJSON(c, http.StatusRequestEntityTooLarge, "body too large")
		}
		h := c.Request().Header
		hook, err := client.VerifyWebhook(h.Get("X-Pusher-Key"), h.Get("X-Pusher-Signature"), body)
		if err != nil {
			return errJSON(c, http.StatusUnauthorized, "invalid webhook")
		}
		if err := fn(c, hook); err != nil {
			c.Logger().Error("hibira webhook handler failed", "error", err)
			return errJSON(c, http.StatusInternalServerError, "webhook handler failed")
		}
		return c.JSON(http.StatusOK, map[string]any{})
	}
}

// ApprovalCallback receives Approvals decisions sent to a request's
// callback_url (or a workspace webhook, with its apwh_ secret). secret is the
// app secret for callback_url deliveries.
func ApprovalCallback(secret string, fn func(c *echo.Context, e *approvals.Event) error) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, ok := readBody(c)
		if !ok {
			return errJSON(c, http.StatusRequestEntityTooLarge, "body too large")
		}
		e := approvals.VerifyCallback(secret, c.Request().Header.Get("Hibira-Signature"), body, time.Now())
		if e == nil {
			return errJSON(c, http.StatusUnauthorized, "invalid signature")
		}
		if err := fn(c, e); err != nil {
			c.Logger().Error("hibira approval callback failed", "error", err)
			return errJSON(c, http.StatusInternalServerError, "callback handler failed")
		}
		return c.JSON(http.StatusOK, map[string]any{})
	}
}

// WaitingRoom lets only visitors with a valid waiting room pass through;
// others go to the hosted queue page (requests other than GET or HEAD get
// 403). Read the pass with Pass.
func WaitingRoom(g waitingroom.Guard) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			d := g.Decide(c.Request())
			switch d.Action {
			case waitingroom.Allow:
				c.Set(PassKey, d.Pass)
				return next(c)
			case waitingroom.Deny:
				c.Response().Header().Set("Cache-Control", "no-store")
				return c.JSON(http.StatusForbidden, map[string]string{"error": "waiting_room", "message": "join the waiting room first"})
			default:
				c.Response().Header().Set("Cache-Control", "no-store")
				if d.Cookie != nil {
					c.SetCookie(d.Cookie)
				}
				return c.Redirect(http.StatusFound, d.Location)
			}
		}
	}
}

// Pass returns the waiting room pass WaitingRoom let through, or nil.
func Pass(c *echo.Context) *waitingroom.Pass {
	p, _ := c.Get(PassKey).(*waitingroom.Pass)
	return p
}
