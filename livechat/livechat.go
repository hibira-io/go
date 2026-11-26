// Package livechat holds server helpers for Hibira Live Chat
// (https://chat.hibira.io).
//
// Verified visitors: sign the signed-in user's ID with the workspace's
// identity secret (Live Chat › Settings › Widget › Verified visitors) and put
// it on the widget's script tag, so the chat shows who they are and nobody
// can pretend to be them.
//
//	<script src="https://chat.hibira.io/w/v1.js" data-workspace="..."
//	        data-user-id="{{.User.ID}}" data-user-hash="{{.UserHash}}"
//	        data-user-name="{{.User.Name}}" data-user-email="{{.User.Email}}" async></script>
//
// Or after the widget loads: HibiraLiveChat.identify({id, hash, name, email}).
package livechat

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
)

// WidgetScript is the Live Chat widget script.
const WidgetScript = "https://chat.hibira.io/w/v1.js"

// UserHash is hex(HMAC-SHA256(identitySecret, userID)), the widget's data-user-hash.
func UserHash(identitySecret, userID string) (string, error) {
	if identitySecret == "" {
		return "", errors.New("livechat: identity secret is required")
	}
	if userID == "" {
		return "", errors.New("livechat: userID is required")
	}
	m := hmac.New(sha256.New, []byte(identitySecret))
	m.Write([]byte(userID))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// Visitor is a signed-in user shown to your team in Live Chat.
type Visitor struct {
	ID    string
	Name  string
	Email string
}

var snippet = template.Must(template.New("w").Parse(
	`<script src="{{.Src}}" data-workspace="{{.Workspace}}"` +
		`{{with .V}}{{if .ID}} data-user-id="{{.ID}}" data-user-hash="{{$.Hash}}"{{if .Name}} data-user-name="{{.Name}}"{{end}}{{if .Email}} data-user-email="{{.Email}}"{{end}}{{end}}{{end}} async></script>`))

// Snippet is the widget's script tag for workspace, ready for an
// html/template page. With a visitor, it signs them in as a verified
// visitor; with nil, they chat as a guest.
func Snippet(workspace, identitySecret string, v *Visitor) (template.HTML, error) {
	if workspace == "" {
		return "", errors.New("livechat: workspace is required")
	}
	data := struct {
		Src, Workspace, Hash string
		V                    *Visitor
	}{Src: WidgetScript, Workspace: workspace, V: v}
	if v != nil && v.ID != "" {
		h, err := UserHash(identitySecret, v.ID)
		if err != nil {
			return "", err
		}
		data.Hash = h
	}
	var b bytes.Buffer
	if err := snippet.Execute(&b, data); err != nil {
		return "", err
	}
	return template.HTML(b.String()), nil //nolint:gosec // built by html/template, which escapes every value
}
