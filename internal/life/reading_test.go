package life

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/model"
)

type feedTransport func(*http.Request) (*http.Response, error)

func (f feedTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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

func TestFeedExcerptTruncationFitsValidationBudget(t *testing.T) {
	text := shorten(strings.Repeat("字", 1300), 1200)
	if len([]rune(text)) != 1200 || !strings.HasSuffix(text, "…") {
		t.Fatalf("truncated excerpt exceeds its schema limit: %d", len([]rune(text)))
	}
}

func TestFetchKeepsChineseAndEntertainmentSourcesInChoicePool(t *testing.T) {
	client := &http.Client{Transport: feedTransport(func(req *http.Request) (*http.Response, error) {
		var items strings.Builder
		for i := 0; i < 5; i++ {
			fmt.Fprintf(&items, "<item><title>%s %d</title><link>https://%s%s/story/%d</link><description>这是公开订阅中的文章摘要，内容涉及当地文化、生活或科学，供角色挑选和记下自己的简短感受。</description></item>", req.URL.Host, i, req.URL.Host, req.URL.Path, i)
		}
		body := "<rss><channel>" + items.String() + "</channel></rss>"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	items, err := Fetch(context.Background(), client)
	if err != nil || len(items) != 12 {
		t.Fatalf("unbalanced feed candidates: %d %v", len(items), err)
	}
	counts := map[string]int{}
	for _, item := range items {
		for _, host := range []string{"sspai.com", "feeds.bbci.co.uk", "www.nasa.gov"} {
			if strings.Contains(item.URL, host) {
				counts[host]++
			}
		}
	}
	if !strings.Contains(items[0].URL, "sspai.com") || counts["sspai.com"] != 3 || counts["www.nasa.gov"] != 3 {
		t.Fatalf("Chinese or international source disappeared: %+v", counts)
	}
}
