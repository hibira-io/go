// Package hibiragin connects Hibira to Gin: channel authorization and user
// sign-in endpoints for pusher-js, webhook and Approvals callback receivers,
// and a waiting room middleware. Your functions get the *gin.Context, so
// whatever your auth middleware stored there is at hand.
//
//	client := hibira.FromEnv()
//	r := gin.Default()
//	api := r.Group("/hibira", requireLogin)
//	api.POST("/auth", hibiragin.Auth(client, func(c *gin.Context, channel string) (bool, *hibira.Member) {
//		u := c.MustGet("user").(*User)
//		if channel == "private-user-"+u.ID {
//			return true, nil
//		}
//		return strings.HasPrefix(channel, "presence-room-"), &hibira.Member{UserID: u.ID, UserInfo: gin.H{"name": u.Name}}
//	}))
//	api.POST("/user-auth", hibiragin.UserAuth(client, func(c *gin.Context) *hibira.User {
//		return &hibira.User{ID: c.MustGet("user").(*User).ID}
//	}))
//	r.POST("/hibira/webhook", hibiragin.Webhook(client, onWebhook))
//	r.GET("/drop", hibiragin.WaitingRoom(room), dropPage)
package hibiragin

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/approvals"
	"github.com/hibira-io/go/waitingroom"
)

// PassKey is the gin.Context key WaitingRoom stores the visitor's pass under.
const PassKey = "hibira.pass"

const maxBody = 1 << 20

func readAuth(c *gin.Context) (hibira.AuthRequest, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return hibira.AuthRequest{}, false
	}
	a, err := hibira.ParseAuthRequest(c.ContentType(), body)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return a, false
	}
	return a, true
}

// Auth answers pusher-js channel authorization requests. fn decides whether
// the request's user may subscribe to channel, and for presence- channels
// returns the member to show.
func Auth(client *hibira.Client, fn func(c *gin.Context, channel string) (bool, *hibira.Member)) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := readAuth(c)
		if !ok {
			return
		}
		allowed, member := false, (*hibira.Member)(nil)
		if a.SocketID != "" && a.Channel != "" {
			allowed, member = fn(c, a.Channel)
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(client.AuthAnswer(a, allowed, member))
	}
}

// UserAuth answers pusher-js user sign-in requests (pusher.signin()), which
// the Chat API and server-to-user events need. fn returns the signed-in user,
// or nil to refuse.
func UserAuth(client *hibira.Client, fn func(c *gin.Context) *hibira.User) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := readAuth(c)
		if !ok {
			return
		}
		var u *hibira.User
		if a.SocketID != "" {
			u = fn(c)
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(client.UserAuthAnswer(a, u))
	}
}

func readBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "body too large"})
		return nil, false
	}
	return body, true
}

// Webhook verifies Hibira webhook deliveries and passes them to fn. A forged
// delivery gets 401; an error from fn answers 500 so Hibira retries.
func Webhook(client *hibira.Client, fn func(c *gin.Context, hook *hibira.Webhook) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, ok := readBody(c)
		if !ok {
			return
		}
		hook, err := client.VerifyWebhook(c.GetHeader("X-Pusher-Key"), c.GetHeader("X-Pusher-Signature"), body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook"})
			return
		}
		if err := fn(c, hook); err != nil {
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "webhook handler failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	}
}

// ApprovalCallback receives Approvals decisions sent to a request's
// callback_url (or a workspace webhook, with its apwh_ secret). secret is the
// app secret for callback_url deliveries.
func ApprovalCallback(secret string, fn func(c *gin.Context, e *approvals.Event) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, ok := readBody(c)
		if !ok {
			return
		}
		e := approvals.VerifyCallback(secret, c.GetHeader("Hibira-Signature"), body, time.Now())
		if e == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
		if err := fn(c, e); err != nil {
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "callback handler failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	}
}

// WaitingRoom lets only visitors with a valid waiting room pass through;
// others go to the hosted queue page (requests other than GET or HEAD get
// 403). Read the pass with Pass.
func WaitingRoom(g waitingroom.Guard) gin.HandlerFunc {
	return func(c *gin.Context) {
		d := g.Decide(c.Request)
		switch d.Action {
		case waitingroom.Allow:
			c.Set(PassKey, d.Pass)
			c.Next()
		case waitingroom.Deny:
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "waiting_room", "message": "join the waiting room first"})
		default:
			c.Header("Cache-Control", "no-store")
			if d.Cookie != nil {
				http.SetCookie(c.Writer, d.Cookie)
			}
			c.Redirect(http.StatusFound, d.Location)
			c.Abort()
		}
	}
}

// Pass returns the waiting room pass WaitingRoom let through, or nil.
func Pass(c *gin.Context) *waitingroom.Pass {
	p, _ := c.Get(PassKey)
	pass, _ := p.(*waitingroom.Pass)
	return pass
}
