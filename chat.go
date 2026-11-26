package hibira

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Chat API: conversations with stored history, read receipts and offline
// push (https://hibira.io/chat-api). Clients receive chat.* events on
// #server-to-user-<user_id> after pusher.signin(); see UserAuthHandler.

// ChatMember is a member of a conversation.
type ChatMember struct {
	UserID  string `json:"user_id"`
	ReadSeq int64  `json:"read_seq"`
	Role    string `json:"role,omitempty"`
}

// Conversation is a Chat API conversation.
type Conversation struct {
	ID        string          `json:"id"`
	Name      string          `json:"name,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Members   []ChatMember    `json:"members,omitempty"`
	Watchers  []string        `json:"watchers,omitempty"`
	LastSeq   int64           `json:"last_seq"`
	Unread    int64           `json:"unread,omitempty"`
	CreatedAt *time.Time      `json:"created_at,omitempty"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
}

// Message is a Chat API message.
type Message struct {
	ID        string              `json:"id"`
	Seq       int64               `json:"seq"`
	Sender    string              `json:"sender,omitempty"`
	Name      string              `json:"name,omitempty"`
	Kind      string              `json:"kind"` // text or system
	Text      string              `json:"text,omitempty"`
	Metadata  json.RawMessage     `json:"metadata,omitempty"`
	VisibleTo []string            `json:"visible_to,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"`
	CreatedAt *time.Time          `json:"created_at,omitempty"`
	EditedAt  *time.Time          `json:"edited_at,omitempty"`
	DeletedAt *time.Time          `json:"deleted_at,omitempty"`
}

// ConversationInput creates or replaces a conversation. Members and
// Watchers left nil keep the current ones.
type ConversationInput struct {
	Name     string            `json:"name,omitempty"`
	Metadata any               `json:"metadata,omitempty"`
	Members  []string          `json:"members,omitempty"`
	Roles    map[string]string `json:"roles,omitempty"`
	Watchers []string          `json:"watchers,omitempty"`
}

// MessageInput is a message sent on a member's behalf.
type MessageInput struct {
	// UserID is the sending member. Required.
	UserID   string `json:"user_id"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Metadata any    `json:"metadata,omitempty"`
	// Push is "offline" (default), "always" or "never".
	Push      string   `json:"push,omitempty"`
	PushTitle string   `json:"push_title,omitempty"`
	PushURL   string   `json:"push_url,omitempty"`
	VisibleTo []string `json:"visible_to,omitempty"`
	// IdempotencyKey makes a retry return the stored message instead of a new one.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// SentMessage is the answer to SendMessage.
type SentMessage struct {
	Message   Message         `json:"message"`
	Duplicate bool            `json:"duplicate,omitempty"`
	Push      json.RawMessage `json:"push,omitempty"`
}

// MessagePage is a page of messages, oldest first.
type MessagePage struct {
	Messages []Message `json:"messages"`
	LastSeq  int64     `json:"last_seq"`
}

// ListOptions picks messages: After a seq (with Limit), or the Last n.
type ListOptions struct {
	After int64
	Limit int
	Last  int
}

// ConversationPage is a user's conversations, latest activity first.
type ConversationPage struct {
	Conversations []Conversation `json:"conversations"`
	// Next goes in Before for the following page; empty on the last one.
	Next string `json:"next,omitempty"`
}

// ChatToken is a token for the Chat API client routes.
type ChatToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func convPath(id, rest string) (string, error) {
	if id == "" {
		return "", errors.New("hibira: conversation ID is required")
	}
	return "/conversations/" + url.PathEscape(id) + rest, nil
}

// ChatRequest makes any Chat API call, signed: path is relative to
// /apps/APP_ID/chat, such as /conversations/ID/typing. out, when not nil,
// receives the JSON answer.
func (c *Client) ChatRequest(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if !strings.HasPrefix(path, "/") {
		return errors.New(`hibira: path must start with "/", such as /conversations/support-42`)
	}
	return c.call(ctx, strings.ToUpper(method), "/chat"+path, query, body, out)
}

func (c *Client) conversationCall(ctx context.Context, method, id, rest string, body any) (*Conversation, error) {
	p, err := convPath(id, rest)
	if err != nil {
		return nil, err
	}
	var out struct {
		Conversation Conversation `json:"conversation"`
	}
	if err := c.ChatRequest(ctx, method, p, nil, body, &out); err != nil {
		return nil, err
	}
	return &out.Conversation, nil
}

// PutConversation creates or replaces a conversation.
func (c *Client) PutConversation(ctx context.Context, id string, in ConversationInput) (*Conversation, error) {
	return c.conversationCall(ctx, "PUT", id, "", in)
}

// GetConversation reads a conversation.
func (c *Client) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	return c.conversationCall(ctx, "GET", id, "", nil)
}

// DeleteConversation deletes a conversation with its members, messages and files.
func (c *Client) DeleteConversation(ctx context.Context, id string) error {
	p, err := convPath(id, "")
	if err != nil {
		return err
	}
	return c.ChatRequest(ctx, "DELETE", p, nil, nil, nil)
}

// AddMembers adds members to a conversation.
func (c *Client) AddMembers(ctx context.Context, id string, userIDs ...string) (*Conversation, error) {
	if len(userIDs) == 0 {
		return nil, errors.New("hibira: give at least one user ID")
	}
	return c.conversationCall(ctx, "POST", id, "/members", map[string]any{"user_ids": userIDs})
}

// RemoveMember removes a member from a conversation.
func (c *Client) RemoveMember(ctx context.Context, id, userID string) (*Conversation, error) {
	if userID == "" {
		return nil, errors.New("hibira: userID is required")
	}
	return c.conversationCall(ctx, "DELETE", id, "/members/"+url.PathEscape(userID), nil)
}

// SendMessage posts a message as a member.
func (c *Client) SendMessage(ctx context.Context, id string, in MessageInput) (*SentMessage, error) {
	if in.UserID == "" {
		return nil, errors.New("hibira: MessageInput.UserID is required")
	}
	p, err := convPath(id, "/messages")
	if err != nil {
		return nil, err
	}
	var out SentMessage
	if err := c.ChatRequest(ctx, "POST", p, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SystemMessage posts a system message (joined, transferred, closed): never
// pushed, never unread.
func (c *Client) SystemMessage(ctx context.Context, id, text string, metadata any) (*Message, error) {
	p, err := convPath(id, "/system")
	if err != nil {
		return nil, err
	}
	body := map[string]any{"text": text}
	if metadata != nil {
		body["metadata"] = metadata
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := c.ChatRequest(ctx, "POST", p, nil, body, &out); err != nil {
		return nil, err
	}
	return &out.Message, nil
}

// ListMessages reads messages, oldest first.
func (c *Client) ListMessages(ctx context.Context, id string, o ListOptions) (*MessagePage, error) {
	p, err := convPath(id, "/messages")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if o.After > 0 {
		q.Set("after", strconv.FormatInt(o.After, 10))
	}
	if o.Limit > 0 {
		q.Set("limit", itoa(o.Limit))
	}
	if o.Last > 0 {
		q.Set("last", itoa(o.Last))
	}
	var out MessagePage
	if err := c.ChatRequest(ctx, "GET", p, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkRead moves how far userID has read to seq and returns the new read_seq.
func (c *Client) MarkRead(ctx context.Context, id, userID string, seq int64) (int64, error) {
	p, err := convPath(id, "/read")
	if err != nil {
		return 0, err
	}
	var out struct {
		ReadSeq int64 `json:"read_seq"`
	}
	if err := c.ChatRequest(ctx, "POST", p, nil, map[string]any{"user_id": userID, "seq": seq}, &out); err != nil {
		return 0, err
	}
	return out.ReadSeq, nil
}

// Typing tells the other members that userID is typing, or stopped.
func (c *Client) Typing(ctx context.Context, id, userID string, typing bool) error {
	p, err := convPath(id, "/typing")
	if err != nil {
		return err
	}
	return c.ChatRequest(ctx, "POST", p, nil, map[string]any{"user_id": userID, "typing": typing}, nil)
}

// UserConversations lists a user's conversations, latest activity first,
// each with its unread count. before is a previous page's Next.
func (c *Client) UserConversations(ctx context.Context, userID string, limit int, before string) (*ConversationPage, error) {
	if userID == "" {
		return nil, errors.New("hibira: userID is required")
	}
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", itoa(limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	var out ConversationPage
	if err := c.ChatRequest(ctx, "GET", "/users/"+url.PathEscape(userID)+"/conversations", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NewChatToken makes a token for the Chat API client routes. conversationID
// limits it to one conversation (join lets the user add themselves); ttl is
// 60 seconds to 24 hours, default 1 hour.
func (c *Client) NewChatToken(ctx context.Context, userID, conversationID string, join bool, ttl time.Duration) (*ChatToken, error) {
	if userID == "" {
		return nil, errors.New("hibira: userID is required")
	}
	body := map[string]any{"user_id": userID}
	if conversationID != "" {
		body["conversation_id"] = conversationID
	}
	if join {
		body["join"] = true
	}
	if ttl > 0 {
		body["ttl"] = int(ttl / time.Second)
	}
	var out ChatToken
	if err := c.ChatRequest(ctx, "POST", "/tokens", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
