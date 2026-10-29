package life

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/model"
)

type choiceModel struct{}

func (choiceModel) Complete(context.Context, string, []model.Message) (string, error) {
	return `{"index":1,"thought":"这段让我想起档案室里那些快没人听的声音","feeling":"有点惦记","question":"有没有办法让人愿意再听一次"}`, nil
}

func TestReadingKeepsSourceSeparateFromPrivateReaction(t *testing.T) {
	raw := []byte(`<rss><channel><item><title>Sound history</title><link>https://example.org/sound</link><description><![CDATA[<p>Old field recordings are being restored for a public archive. The tapes include coastal bird calls and local interviews.</p>]]></description></item></channel></rss>`)
	items, err := parseFeed(raw)
	if err != nil || len(items) != 1 || strings.Contains(items[0].Excerpt, "<p>") {
		t.Fatalf("RSS excerpt not parsed: %+v, %v", items, err)
	}
	at := time.Date(2026, 10, 29, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	reading, err := Choose(context.Background(), choiceModel{}, "fake", "喜欢旧录音", "zh-CN", at, nil, items)
	if err != nil || reading.Title != "Sound history" || reading.URL != "https://example.org/sound" || reading.Excerpt != items[0].Excerpt || reading.Thought == "" {
		t.Fatalf("source and reaction were not kept apart: %+v, %v", reading, err)
	}
}
