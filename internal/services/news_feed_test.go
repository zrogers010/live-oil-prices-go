package services

import (
	"fmt"
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	got := slugify("Brent Crude Oil + OPEC Update!!")
	want := "brent-crude-oil-opec-update"
	if got != want {
		t.Fatalf("slugify=%q, want %q", got, want)
	}
}

func TestStripHTMLAndReadTime(t *testing.T) {
	text := stripHTML("<p>Hello &amp; <strong>world</strong>  from <em>Oil</em></p>")
	if text != "Hello & world from Oil" {
		t.Fatalf("stripHTML=%q", text)
	}

	readTime := estimateReadTime("one two three four five")
	if readTime != "1 min read" {
		t.Fatalf("expected minimum 1 min read, got %q", readTime)
	}

	longText := strings.Repeat("word ", 250)
	readTime = estimateReadTime(longText)
	if readTime != "1 min read" {
		t.Fatalf("expected 1 min read, got %q", readTime)
	}
}

func TestHashIDStable(t *testing.T) {
	id1 := hashID("https://example.com/news/1")
	id2 := hashID("https://example.com/news/1")
	if id1 != id2 {
		t.Fatalf("hashID expected stable output")
	}
	if len(id1) != 16 {
		t.Fatalf("expected 16-char hash, got %d", len(id1))
	}
}

func TestParseRSSDateSupportsKnownFormats(t *testing.T) {
	pairs := []string{
		"Mon, 01 Jan 2024 15:04:05 -0700",
		"Mon, 01 Jan 2024 15:04:05 MST",
		time.Now().Format(time.RFC1123Z),
		"2024-01-01T15:04:05Z",
	}
	for _, value := range pairs {
		if got := parseRSSDate(value); got.IsZero() {
			t.Fatalf("expected parsed time for %q", value)
		}
	}

	invalid := parseRSSDate("not a valid date")
	if time.Since(invalid) > 2*time.Second {
		t.Fatalf("expected fallback to now for invalid date")
	}
}

func TestFetchFeedParsesAndFiltersItems(t *testing.T) {
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
	<rss>
	  <channel>
		<item>
		  <title>Oil Supply Tightens Ahead</title>
		  <link>https://example.com/supply</link>
		  <description><![CDATA[<p>OPEC production output is tight and stocks are low.</p>]]></description>
		  <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
		  <source url="https://example.com">Reuters</source>
		</item>
		<item>
		  <title></title>
		  <link>https://example.com/invalid</link>
		  <description>Skip this item</description>
		  <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
		  <source url="https://example.com">Reuters</source>
		</item>
		<item>
		  <title>OilPrice Report: Another Article</title>
		  <link>https://example.com/oilprice</link>
		  <description>Daily updates from OilPrice</description>
		  <pubDate>Mon, 01 Jan 2024 11:00:00 +0000</pubDate>
		  <source url="https://example.com">OilPrice News</source>
		</item>
	  </channel>
	</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, rssBody)
	}))
	defer srv.Close()

	svc := &NewsFeedService{
		client: srv.Client(),
	}
	articles, err := svc.fetchFeed(feedSource{url: srv.URL, category: "Oil Markets"})
	if err != nil {
		t.Fatalf("fetchFeed returned error: %v", err)
	}

	if len(articles) != 1 {
		t.Fatalf("expected 1 article after filtering, got %d", len(articles))
	}

	article := articles[0]
	if article.Source != "Reuters" {
		t.Fatalf("expected source Reuters, got %q", article.Source)
	}
	if !strings.Contains(article.Slug, "oil-supply-tightens-ahead") {
		t.Fatalf("unexpected slug %q", article.Slug)
	}
	if article.Summary == "" {
		t.Fatalf("expected summary populated")
	}
	if article.ReadTime == "" {
		t.Fatalf("expected readTime populated")
	}
}

func TestRefreshDeduplicatesAndSortsByPublishedAt(t *testing.T) {
	rssBody := `<?xml version="1.0" encoding="UTF-8"?>
	<rss>
	  <channel>
		<item>
		  <title>Brent Spikes on Demand</title>
		  <link>https://example.com/brent1</link>
		  <description>Markets are tight.</description>
		  <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
		  <source url="https://example.com">Reuters</source>
		</item>
		<item>
		  <title>Brent Slides on Inventory</title>
		  <link>https://example.com/brent2</link>
		  <description>Inventory data comes in.</description>
		  <pubDate>Mon, 01 Jan 2024 11:30:00 +0000</pubDate>
		  <source url="https://example.com">Reuters</source>
		</item>
		<item>
		  <title>Brent Slides on Inventory</title>
		  <link>https://example.com/brent2</link>
		  <description>Inventory data comes in.</description>
		  <pubDate>Mon, 01 Jan 2024 11:30:00 +0000</pubDate>
		  <source url="https://example.com">Reuters</source>
		</item>
	  </channel>
	</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, rssBody)
	}))
	defer srv.Close()

	svc := &NewsFeedService{
		client: srv.Client(),
		feeds: []feedSource{
			{url: srv.URL + "/feedA", category: "Oil Markets"},
			{url: srv.URL + "/feedB", category: "Oil Markets"},
		},
	}
	svc.refresh()
	articles := svc.GetNews()

	if len(articles) != 2 {
		t.Fatalf("expected 2 deduplicated articles, got %d", len(articles))
	}

	first, err := time.Parse(time.RFC3339, articles[0].PublishedAt)
	if err != nil {
		t.Fatalf("invalid first publishedAt: %v", err)
	}
	second, err := time.Parse(time.RFC3339, articles[1].PublishedAt)
	if err != nil {
		t.Fatalf("invalid second publishedAt: %v", err)
	}

	if first.Before(second) {
		t.Fatalf("articles should be sorted newest first")
	}
}

func TestGetNewsAndByID(t *testing.T) {
	svc := &NewsFeedService{
		articles: []models.NewsArticle{
			{
				ID:        "id-1",
				Slug:      "slug-1",
				Title:     "Title One",
				PublishedAt: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
	}

	all := svc.GetNews()
	if len(all) != 1 {
		t.Fatalf("expected 1 article")
	}
	all[0].Title = "Modified"
	if svc.articles[0].Title != "Title One" {
		t.Fatalf("expected GetNews to return copy, underlying store changed")
	}

	byID := svc.GetNewsByID("id-1")
	if byID == nil || byID.Title != "Title One" {
		t.Fatalf("expected to find by id")
	}

	bySlug := svc.GetNewsByID("slug-1")
	if bySlug == nil || bySlug.ID != "id-1" {
		t.Fatalf("expected to find by slug")
	}

	if missing := svc.GetNewsByID("does-not-exist"); missing != nil {
		t.Fatalf("expected nil for missing article")
	}
}
