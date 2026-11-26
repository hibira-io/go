package livechat

import (
	"strings"
	"testing"
)

func TestUserHash(t *testing.T) {
	// hex(HMAC-SHA256("secret", "42")), the value every Hibira SDK computes.
	h, err := UserHash("secret", "42")
	if err != nil {
		t.Fatal(err)
	}
	if h != "93c121e7aa437a1e01e3c512c6f0ce3c821a839025dca4408f85616de4aaee70" {
		t.Fatalf("hash %s", h)
	}
	if _, err := UserHash("", "42"); err == nil {
		t.Fatal("empty secret")
	}
	if _, err := UserHash("secret", ""); err == nil {
		t.Fatal("empty user")
	}
}

func TestSnippet(t *testing.T) {
	s, err := Snippet("ws_1", "secret", &Visitor{ID: "42", Name: `Ann "A" <b>`, Email: "ann@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := UserHash("secret", "42")
	for _, want := range []string{`src="https://chat.hibira.io/w/v1.js"`, `data-workspace="ws_1"`, `data-user-id="42"`, `data-user-hash="` + h + `"`, `data-user-name="Ann &#34;A&#34; &lt;b&gt;"`, ` async></script>`} {
		if !strings.Contains(string(s), want) {
			t.Fatalf("%s missing %s", s, want)
		}
	}
	guest, _ := Snippet("ws_1", "", nil)
	if strings.Contains(string(guest), "data-user") {
		t.Fatalf("guest %s", guest)
	}
}
