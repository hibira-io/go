package stream_test

import (
	"context"
	"log"

	hibira "github.com/hibira-io/go"
	"github.com/hibira-io/go/stream"
)

// Write each text delta from your LLM SDK as it arrives, then Close.
func Example() {
	client := hibira.FromEnv()
	ctx := context.Background()

	s, err := stream.New(ctx, client, "private-conversation-42", stream.Options{Meta: map[string]string{"role": "assistant"}})
	if err != nil {
		log.Fatal(err)
	}
	for _, delta := range []string{"Hel", "lo", "!"} { // e.g. each text delta of an OpenAI or Anthropic stream
		if _, err := s.WriteString(delta); err != nil {
			break
		}
	}
	if err := s.Close(); err != nil {
		log.Print(err)
	}
	log.Println(s.ID(), s.Text())
}
