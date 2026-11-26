// Package hibirafiber connects Hibira to Fiber (v3): channel authorization
// and user sign-in endpoints for pusher-js, webhook and Approvals callback
// receivers, and a waiting room middleware. Your functions get the
// fiber.Ctx, so whatever your auth middleware stored in Locals is at hand.
//
//	client := hibira.FromEnv()
//	app := fiber.New()
//	g := app.Group("/hibira", requireLogin)
//	g.Post("/auth", hibirafiber.Auth(client, func(c fiber.Ctx, channel string) (bool, *hibira.Member) {
//		u := c.Locals("user").(*User)
//		if channel == "private-user-"+u.ID {
//			return true, nil
//		}
//		return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: fiber.Map{"name": u.Name}}
//	}))
//	g.Post("/user-auth", hibirafiber.UserAuth(client, func(c fiber.Ctx) *hibira.User {
//		return &hibira.User{ID: c.Locals("user").(*User).ID}
//	}))
//	app.Post("/hibira/webhook", hibirafiber.Webhook(client, onWebhook))
//	app.Get("/drop", hibirafiber.WaitingRoom(room), dropPage)
//
// On Fiber v2, mount the net/http handlers of github.com/hibira-io/go with
// the adaptor middleware (adaptor.HTTPHandler).
package hibirafiber

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/waitingroom"
)

// PassKey is the Locals key WaitingRoom stores the visitor's pass under.
const PassKey = "hibira.pass"

const maxBody = 1 << 20

func errJSON(c fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

func reply(c fiber.Ctx, status int, body any) error {
	c.Set("Cache-Control", "no-store")
	return c.Status(status).JSON(body)
}

// Auth answers pusher-js channel authorization requests. fn decides whether
// the request's user may subscribe to channel, and for presence- channels
// returns the member to show.
func Auth(client *hibira.Client, fn func(c fiber.Ctx, channel string) (bool, *hibira.Member)) fiber.Handler {
	return func(c fiber.Ctx) error {
		a, err := hibira.ParseAuthRequest(c.Get(fiber.HeaderContentType), c.Body())
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "bad request")
		}
		allowed, member := false, (*hibira.Member)(nil)
		if a.SocketID != "" && a.Channel != "" {
			allowed, member = fn(c, a.Channel)
		}
		status, body := client.AuthAnswer(a, allowed, member)
		return reply(c, status, body)
	}
}

// UserAuth answers pusher-js user sign-in requests (pusher.signin()), which
// the Chat API and server-to-user events need. fn returns the signed-in user,
// or nil to refuse.
func UserAuth(client *hibira.Client, fn func(c fiber.Ctx) *hibira.User) fiber.Handler {
	return func(c fiber.Ctx) error {
		a, err := hibira.ParseAuthRequest(c.Get(fiber.HeaderContentType), c.Body())
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "bad request")
		}
		var u *hibira.User
		if a.SocketID != "" {
			u = fn(c)
		}
		status, body := client.UserAuthAnswer(a, u)
		return reply(c, status, body)
	}
}

// Webhook verifies Hibira webhook deliveries and passes them to fn. A forged
// delivery gets 401; an error from fn answers 500 so Hibira retries.
func Webhook(client *hibira.Client, fn func(c fiber.Ctx, hook *hibira.Webhook) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if len(body) > maxBody {
			return errJSON(c, http.StatusRequestEntityTooLarge, "body too large")
		}
		hook, err := client.VerifyWebhook(c.Get("X-Pusher-Key"), c.Get("X-Pusher-Signature"), body)
		if err != nil {
			return errJSON(c, http.StatusUnauthorized, "invalid webhook")
		}
		if err := fn(c, hook); err != nil {
			return errJSON(c, http.StatusInternalServerError, "webhook handler failed")
		}
		return c.JSON(fiber.Map{})
	}
}

// ApprovalCallback receives Approvals decisions sent to a request's
// callback_url (or a workspace webhook, with its apwh_ secret). secret is the
// app secret for callback_url deliveries.
func ApprovalCallback(secret string, fn func(c fiber.Ctx, e *approvals.Event) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if len(body) > maxBody {
			return errJSON(c, http.StatusRequestEntityTooLarge, "body too large")
		}
		e := approvals.VerifyCallback(secret, c.Get("Hibira-Signature"), body, time.Now())
		if e == nil {
			return errJSON(c, http.StatusUnauthorized, "invalid signature")
		}
		if err := fn(c, e); err != nil {
			return errJSON(c, http.StatusInternalServerError, "callback handler failed")
		}
		return c.JSON(fiber.Map{})
	}
}

// WaitingRoom lets only visitors with a valid waiting room pass through;
// others go to the hosted queue page (requests other than GET or HEAD get
// 403). Read the pass with Pass.
func WaitingRoom(g waitingroom.Guard) fiber.Handler {
	return func(c fiber.Ctx) error {
		r, err := adaptor.ConvertRequest(c, false)
		if err != nil {
			return err
		}
		d := g.Decide(r)
		switch d.Action {
		case waitingroom.Allow:
			c.Locals(PassKey, d.Pass)
			return c.Next()
		case waitingroom.Deny:
			return reply(c, http.StatusForbidden, fiber.Map{"error": "waiting_room", "message": "join the waiting room first"})
		default:
			c.Set("Cache-Control", "no-store")
			if k := d.Cookie; k != nil {
				c.Cookie(&fiber.Cookie{
					Name: k.Name, Value: k.Value, Path: k.Path, MaxAge: k.MaxAge,
					HTTPOnly: k.HttpOnly, Secure: k.Secure, SameSite: fiber.CookieSameSiteLaxMode,
				})
			}
			return c.Redirect().Status(http.StatusFound).To(d.Location)
		}
	}
}

// Pass returns the waiting room pass WaitingRoom let through, or nil.
func Pass(c fiber.Ctx) *waitingroom.Pass {
	p, _ := c.Locals(PassKey).(*waitingroom.Pass)
	return p
}
