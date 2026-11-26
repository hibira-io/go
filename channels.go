package hibira

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// MaxTriggerChannels is how many channels one event may go to.
const MaxTriggerChannels = 100

var (
	channelRE  = regexp.MustCompile(`^[-a-zA-Z0-9_=@,.;]+$`)
	socketIDRE = regexp.MustCompile(`^\d+\.\d+$`)
)

func checkChannel(ch string) error {
	if len(ch) > 200 || !channelRE.MatchString(ch) {
		return fmt.Errorf("hibira: invalid channel name %q", ch)
	}
	return nil
}

func checkSocketID(id string) error {
	if !socketIDRE.MatchString(id) {
		return fmt.Errorf("hibira: invalid socket_id %q", id)
	}
	return nil
}

// Event is one event to publish.
type Event struct {
	// Channels are 1 to 100 channel names.
	Channels []string
	// Name is the event name, 1 to 200 characters.
	Name string
	// Data is sent as is when it is a string or []byte, otherwise as JSON.
	Data any
	// SocketID, when set, leaves out that connection (the sender's own tab).
	SocketID string
}

// BatchEvent is one event of TriggerBatch.
type BatchEvent struct {
	Channel  string
	Name     string
	Data     any
	SocketID string
}

func encodeData(d any) (string, error) {
	switch v := d.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	}
	b, err := compact(d)
	return string(b), err
}

// Trigger publishes an event to one channel. Data that is not a string is
// sent as JSON.
func (c *Client) Trigger(ctx context.Context, channel, event string, data any) error {
	return c.Publish(ctx, Event{Channels: []string{channel}, Name: event, Data: data})
}

// Publish publishes an event to up to 100 channels.
func (c *Client) Publish(ctx context.Context, e Event) error {
	if len(e.Channels) == 0 || len(e.Channels) > MaxTriggerChannels {
		return fmt.Errorf("hibira: an event goes to 1 to %d channels", MaxTriggerChannels)
	}
	for _, ch := range e.Channels {
		if err := checkChannel(ch); err != nil {
			return err
		}
	}
	if e.Name == "" || len(e.Name) > 200 {
		return errors.New("hibira: event name must be 1 to 200 characters")
	}
	data, err := encodeData(e.Data)
	if err != nil {
		return err
	}
	body := map[string]any{"name": e.Name, "data": data, "channels": e.Channels}
	if e.SocketID != "" {
		if err := checkSocketID(e.SocketID); err != nil {
			return err
		}
		body["socket_id"] = e.SocketID
	}
	return c.call(ctx, "POST", "/events", nil, body, nil)
}

// TriggerBatch publishes several events in one request (10 at most by default).
func (c *Client) TriggerBatch(ctx context.Context, events []BatchEvent) error {
	if len(events) == 0 {
		return errors.New("hibira: give at least one event")
	}
	batch := make([]map[string]string, 0, len(events))
	for _, e := range events {
		if err := checkChannel(e.Channel); err != nil {
			return err
		}
		data, err := encodeData(e.Data)
		if err != nil {
			return err
		}
		item := map[string]string{"channel": e.Channel, "name": e.Name, "data": data}
		if e.SocketID != "" {
			if err := checkSocketID(e.SocketID); err != nil {
				return err
			}
			item["socket_id"] = e.SocketID
		}
		batch = append(batch, item)
	}
	return c.call(ctx, "POST", "/batch_events", nil, map[string]any{"batch": batch}, nil)
}

// ChannelInfo describes an occupied channel. UserCount (presence channels)
// and SubscriptionCount are only filled when asked for with info.
type ChannelInfo struct {
	Occupied          bool `json:"occupied"`
	UserCount         int  `json:"user_count,omitempty"`
	SubscriptionCount int  `json:"subscription_count,omitempty"`
}

func infoQuery(info []string) url.Values {
	q := url.Values{}
	if len(info) > 0 {
		q.Set("info", strings.Join(info, ","))
	}
	return q
}

// Channels lists the occupied channels whose names start with prefix (all
// when empty). info may be "user_count" (presence- prefixes only).
func (c *Client) Channels(ctx context.Context, prefix string, info ...string) (map[string]ChannelInfo, error) {
	q := infoQuery(info)
	if prefix != "" {
		q.Set("filter_by_prefix", prefix)
	}
	var out struct {
		Channels map[string]ChannelInfo `json:"channels"`
	}
	if err := c.call(ctx, "GET", "/channels", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Channels == nil {
		out.Channels = map[string]ChannelInfo{}
	}
	return out.Channels, nil
}

// Channel reads one channel. info may hold "user_count" and "subscription_count".
func (c *Client) Channel(ctx context.Context, channel string, info ...string) (*ChannelInfo, error) {
	if err := checkChannel(channel); err != nil {
		return nil, err
	}
	var out ChannelInfo
	if err := c.call(ctx, "GET", "/channels/"+channel, infoQuery(info), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Users lists the user IDs subscribed to a presence channel.
func (c *Client) Users(ctx context.Context, channel string) ([]string, error) {
	if err := checkChannel(channel); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(channel, "presence-") {
		return nil, errors.New("hibira: Users works on presence- channels")
	}
	var out struct {
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
	}
	if err := c.call(ctx, "GET", "/channels/"+channel+"/users", nil, nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, len(out.Users))
	for i, u := range out.Users {
		ids[i] = u.ID
	}
	return ids, nil
}

// TerminateUserConnections disconnects every connection signed in as userID.
func (c *Client) TerminateUserConnections(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("hibira: userID is required")
	}
	return c.call(ctx, "POST", "/users/"+url.PathEscape(userID)+"/terminate_connections", nil, map[string]any{}, nil)
}
