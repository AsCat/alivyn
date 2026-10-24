package sharedrelay_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/sharedclient"
	"github.com/AsCat/alivyn/internal/sharedrelay"
)

type sentMessage struct{ chatID, text string }
type fakeSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (f *fakeSender) Send(_ context.Context, chatID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID, text})
	return nil
}

func TestSharedPairingRoutesOnlyToBoundAgent(t *testing.T) {
	ctx := context.Background()
	sender := &fakeSender{}
	r, err := sharedrelay.Open(ctx, t.TempDir(), "https://applink.feishu.cn/example", sender)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	create := sharedclient.Client{BaseURL: server.URL}
	first, err := create.Pair(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := create.Pair(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clientA := sharedclient.Client{BaseURL: server.URL, Token: first.Token}
	clientB := sharedclient.Client{BaseURL: server.URL, Token: second.Token}
	if paired, err := clientA.Status(ctx); err != nil || paired {
		t.Fatalf("new agent should be unpaired: %t, %v", paired, err)
	}
	if err := clientA.Send(ctx, "hello"); err == nil {
		t.Fatal("unpaired agent sent a message")
	}
	for _, item := range []struct {
		openID, chatID, eventID, messageID, code string
	}{
		{"ou_a", "oc_a", "pair-a", "m-pair-a", first.Code},
		{"ou_b", "oc_b", "pair-b", "m-pair-b", second.Code},
	} {
		ack, err := r.Incoming(ctx, item.openID, sharedrelay.Message{
			EventID: item.eventID, ChatID: item.chatID, MessageID: item.messageID, Text: item.code,
		}, time.Now())
		if err != nil || ack == "" {
			t.Fatalf("pairing failed: %q, %v", ack, err)
		}
	}
	if paired, err := clientA.Status(ctx); err != nil || !paired {
		t.Fatalf("agent A was not paired: %t, %v", paired, err)
	}
	// Feishu may redeliver the pairing event; it must not enter AI history.
	if _, err := r.Incoming(ctx, "ou_a", sharedrelay.Message{
		EventID: "pair-a-retry", ChatID: "oc_a", MessageID: "m-pair-a", Text: first.Code,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ openID, chatID, eventID, text string }{
		{"ou_a", "oc_a", "a1", "hello A"},
		{"ou_b", "oc_b", "b1", "hello B"},
	} {
		_, err := r.Incoming(ctx, item.openID, sharedrelay.Message{
			EventID: item.eventID, ChatID: item.chatID, MessageID: "m-" + item.eventID, Text: item.text,
		}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	gotA, err := clientA.Poll(ctx)
	if err != nil || len(gotA) != 1 || gotA[0].Text != "hello A" {
		t.Fatalf("wrong A messages: %+v, %v", gotA, err)
	}
	gotB, err := clientB.Poll(ctx)
	if err != nil || len(gotB) != 1 || gotB[0].Text != "hello B" {
		t.Fatalf("wrong B messages: %+v, %v", gotB, err)
	}
	// Poll redelivers until the local agent confirms durable storage.
	redelivered, err := clientA.Poll(ctx)
	if err != nil || len(redelivered) != 1 || redelivered[0].EventID != "a1" {
		t.Fatalf("message was not redelivered before ack: %+v, %v", redelivered, err)
	}
	if err := clientA.Ack(ctx, []string{"a1"}); err != nil {
		t.Fatal(err)
	}
	if err := clientA.Send(ctx, "reply A"); err != nil {
		t.Fatal(err)
	}
	if err := clientB.Send(ctx, "reply B"); err != nil {
		t.Fatal(err)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 2 || sender.sent[0].chatID != "oc_a" || sender.sent[1].chatID != "oc_b" {
		t.Fatalf("outbound messages crossed users: %+v", sender.sent)
	}
	if _, err := (sharedclient.Client{BaseURL: server.URL, Token: "wrong-token-that-is-long-enough-to-check"}).Status(ctx); err == nil {
		t.Fatal("bad token was accepted")
	}
}

func TestPairCodeExpiresAndCannotBeReused(t *testing.T) {
	ctx := context.Background()
	r, err := sharedrelay.Open(ctx, t.TempDir(), "https://applink.feishu.cn/example", &fakeSender{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	old := time.Now().Add(-11 * time.Minute)
	pair, err := r.CreatePair(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := r.Incoming(ctx, "ou_a", sharedrelay.Message{EventID: "e1", ChatID: "oc_a", MessageID: "m1", Text: pair.Code}, time.Now())
	if err != nil || ack == "" {
		t.Fatalf("expired code was not rejected: %q, %v", ack, err)
	}
	id, err := r.Authenticate(ctx, pair.Token)
	if err != nil {
		t.Fatal(err)
	}
	if paired, err := r.Status(ctx, id); err != nil || paired {
		t.Fatalf("expired code paired: %t, %v", paired, err)
	}
	fresh, err := r.CreatePair(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Incoming(ctx, "ou_b", sharedrelay.Message{EventID: "e2", ChatID: "oc_b", MessageID: "m2", Text: fresh.Code}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Incoming(ctx, "ou_c", sharedrelay.Message{EventID: "e3", ChatID: "oc_c", MessageID: "m3", Text: fresh.Code}, time.Now()); err != nil {
		t.Fatal(err)
	}
	otherID, err := r.Authenticate(ctx, fresh.Token)
	if err != nil {
		t.Fatal(err)
	}
	if paired, err := r.Status(ctx, otherID); err != nil || !paired {
		t.Fatalf("fresh code lost owner: %t, %v", paired, err)
	}
}

func TestNewCodeTransfersExistingFeishuBinding(t *testing.T) {
	ctx := context.Background()
	r, err := sharedrelay.Open(ctx, t.TempDir(), "https://applink.feishu.cn/example", &fakeSender{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now()
	first, err := r.CreatePair(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Incoming(ctx, "ou_owner", sharedrelay.Message{EventID: "pair-1", ChatID: "oc_owner", MessageID: "m1", Text: first.Code}, now); err != nil {
		t.Fatal(err)
	}
	second, err := r.CreatePair(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Incoming(ctx, "ou_owner", sharedrelay.Message{EventID: "pair-2", ChatID: "oc_owner", MessageID: "m2", Text: second.Code}, now); err != nil {
		t.Fatal(err)
	}
	oldID, _ := r.Authenticate(ctx, first.Token)
	newID, _ := r.Authenticate(ctx, second.Token)
	oldPaired, _ := r.Status(ctx, oldID)
	newPaired, _ := r.Status(ctx, newID)
	if oldPaired || !newPaired {
		t.Fatalf("binding did not transfer: old=%t new=%t", oldPaired, newPaired)
	}
	if err := r.Send(ctx, oldID, "old agent"); err == nil {
		t.Fatal("old agent could still send after re-pair")
	}
}
