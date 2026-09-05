package goal

import (
	"context"
	"testing"
)

type fakeEval struct{ next Evaluation }

func (f fakeEval) Evaluate(context.Context, string, string) (Evaluation, error) { return f.next, nil }
func TestControllerBlocksThenAllows(t *testing.T) {
	c := NewController(fakeEval{next: Evaluation{OK: false, Reason: "need test"}}, 3, 5)
	if err := c.Set("tests pass"); err != nil {
		t.Fatal(err)
	}
	if d, _, _ := c.Evaluate(context.Background(), "x"); d != "block" {
		t.Fatal(d)
	}
	c.Eval = fakeEval{next: Evaluation{OK: true, Reason: "test output present"}}
	if d, _, _ := c.Evaluate(context.Background(), "test exit 0"); d != "allow" {
		t.Fatal(d)
	}
}
