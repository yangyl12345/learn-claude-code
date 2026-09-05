package permission

import (
	"context"
	"strings"
	"testing"
)

func TestPolicy(t *testing.T) {
	p := NewPolicy("/workspace")
	if p.Check("rm -rf build") != Deny {
		t.Fatal("dangerous command allowed")
	}
	if p.Check("git push origin main") != Confirm {
		t.Fatal("push should confirm")
	}
	if p.Check("printf ok") != Allow {
		t.Fatal("safe command not allowed")
	}
}
func TestBroker(t *testing.T) {
	b := ConsoleBroker{In: strings.NewReader("yes\n")}
	ok, e := b.Ask(context.Background(), "continue?")
	if e != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, e)
	}
}
