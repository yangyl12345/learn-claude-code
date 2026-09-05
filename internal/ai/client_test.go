package ai

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMessagesAndFunctionOutputsAreStableJSON(t *testing.T) {
	user := UserMessage("hello")
	var decoded map[string]any
	if err := json.Unmarshal(user, &decoded); err != nil || decoded["role"] != "user" {
		t.Fatalf("bad user item: %s", user)
	}
	output := FunctionCallOutput("call_123", "result")
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "function_call_output" || decoded["call_id"] != "call_123" {
		t.Fatalf("bad function output: %v", decoded)
	}
}

func TestFakeClientRecordsAndReturnsResponses(t *testing.T) {
	f := &FakeClient{Responses: []Response{{Text: "ok", InputTokens: 2, OutputTokens: 3}}}
	r, err := f.CreateResponse(context.Background(), Request{Model: "test"})
	if err != nil || r.Text != "ok" || len(f.Calls) != 1 {
		t.Fatalf("response=%+v calls=%d err=%v", r, len(f.Calls), err)
	}
}
