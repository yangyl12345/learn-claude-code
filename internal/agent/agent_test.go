package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yangyl12345/learn-claude-code/internal/ai"
)

func TestLoopExecutesMultipleCallsInOrder(t *testing.T) {
	var order []string
	r := NewRegistry()
	for _, name := range []string{"first", "second"} {
		name := name
		if err := r.Register(Tool{Definition: ai.ToolDefinition{Name: name}, Handler: func(_ context.Context, _ json.RawMessage) (string, error) {
			order = append(order, name)
			return name + "-done", nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	f := &ai.FakeClient{Responses: []ai.Response{
		{Output: []ai.OutputItem{{Type: "function_call", CallID: "c1", Name: "first", ArgumentsJSON: `{}`}, {Type: "function_call", CallID: "c2", Name: "second", ArgumentsJSON: `{}`}}},
		{Text: "finished"},
	}}
	got, err := (&Loop{Client: f, Tools: r, Config: Config{Model: "fake", MaxTurns: 3}}).Run(context.Background(), "do it")
	if err != nil || got.Text != "finished" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("order=%v", order)
	}
	if len(f.Calls) != 2 || len(f.Calls[1].Input) != 5 {
		t.Fatalf("history did not preserve calls/results: %+v", f.Calls[1].Input)
	}
}

func TestLoopReturnsUnknownToolToModel(t *testing.T) {
	f := &ai.FakeClient{Responses: []ai.Response{{Output: []ai.OutputItem{{Type: "function_call", CallID: "c1", Name: "missing", ArgumentsJSON: `{}`}}}, {Text: "handled"}}}
	got, err := (&Loop{Client: f, Tools: NewRegistry(), Config: Config{Model: "fake", MaxTurns: 2}}).Run(context.Background(), "x")
	if err != nil || got.Text != "handled" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if len(f.Calls) != 2 || !strings.Contains(string(f.Calls[1].Input[len(f.Calls[1].Input)-1]), "unknown tool") {
		t.Fatalf("missing tool result")
	}
}
