package services

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"live-oil-prices-go/internal/models"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '-' {
			return '-'
		}
		return -1
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	PubDate     string    `xml:"pubDate"`
	Source      rssSource `xml:"source"`
}

type rssSource struct {
	Name string `xml:",chardata"`
	URL  string `xml:"url,attr"`
}

type feedSource struct {
	url      string
	category string
}

type NewsFeedService struct {
	mu       sync.RWMutex
	articles []models.NewsArticle
	client   *http.Client
	feeds    []feedSource
}

const gnewsBase = "https://news.google.com/rss/search?hl=en-US&gl=US&ceid=US:en&q="

func NewNewsFeedService() *NewsFeedService {
	svc := &NewsFeedService{
		client: &http.Client{Timeout: 15 * time.Second},
		feeds: []feedSource{
			{url: gnewsBase + "crude+oil+price+WTI+Brent", category: "Oil Markets"},
			{url: gnewsBase + "OPEC+oil+production+output", category: "OPEC"},
			{url: gnewsBase + "natural+gas+LNG+Henry+Hub", category: "Natural Gas"},
			{url: gnewsBase + "oil+refining+gasoline+diesel+fuel", category: "Refining"},
			{url: gnewsBase + "oil+drilling+extraction+upstream+shale", category: "Extraction"},
			{url: gnewsBase + "oil+gas+engineering+technology+energy+innovation", category: "Technology"},
			{url: gnewsBase + "international+energy+policy+geopolitics+oil+sanctions", category: "International"},
			{url: gnewsBase + "oil+gas+inventory+EIA+stockpile+storage", category: "Inventory"},
		},
	}

	go svc.refresh()

	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			svc.refresh()
		}
	}()

	return svc
}

func (s *NewsFeedService) refresh() {
	var allArticles []models.NewsArticle
	seen := make(map[string]bool)
	successCount := 0

	for _, feed := range s.feeds {
		articles, err := s.fetchFeed(feed)
		if err != nil {
			log.Printf("RSS fetch error [%s]: %v", feed.category, err)
			continue
		}
		successCount++
		for _, a := range articles {
			key := a.SourceURL
			if key == "" {
				key = a.Title
			}
			if !seen[key] {
				seen[key] = true
				allArticles = append(allArticles, a)
			}
		}
	}

	sort.Slice(allArticles, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, allArticles[i].PublishedAt)
		tj, _ := time.Parse(time.RFC3339, allArticles[j].PublishedAt)
		return ti.After(tj)
	})

	if len(allArticles) > 80 {
		allArticles = allArticles[:80]
	}

	s.mu.Lock()
	s.articles = allArticles
	s.mu.Unlock()

	log.Printf("News feed refreshed: %d articles from %d/%d feeds", len(allArticles), successCount, len(s.feeds))
}

func (s *NewsFeedService) fetchFeed(feed feedSource) ([]models.NewsArticle, error) {
	req, err := http.NewRequest("GET", feed.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; LiveOilPrices/1.0)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}

	var rss rssFeed
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("xml parse: %w", err)
	}

	limit := 10
	if len(rss.Channel.Items) < limit {
		limit = len(rss.Channel.Items)
	}

	articles := make([]models.NewsArticle, 0, limit)
	for _, item := range rss.Channel.Items[:limit] {
		title := html.UnescapeString(strings.TrimSpace(item.Title))
		if title == "" {
			continue
		}

		summary := stripHTML(item.Description)
		if len(summary) > 500 {
			summary = summary[:500] + "..."
		}

		source := html.UnescapeString(item.Source.Name)
		if source == "" {
			source = "News"
		}

		if strings.Contains(strings.ToLower(source), "oilprice") {
			continue
		}

		pubTime := parseRSSDate(item.PubDate)

		category := refineCategoryFromContent(title, summary, feed.category)

		articles = append(articles, models.NewsArticle{
			ID:          hashID(item.Link),
			Slug:        slugify(title),
			Title:       title,
			Summary:     summary,
			Content:     summary,
			Source:      source,
			SourceURL:   strings.TrimSpace(item.Link),
			Category:    category,
			PublishedAt: pubTime.Format(time.RFC3339),
			ReadTime:    estimateReadTime(summary),
		})
	}

	return articles, nil
}

func (s *NewsFeedService) GetNews() []models.NewsArticle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.NewsArticle, len(s.articles))
	copy(result, s.articles)
	return result
}

func (s *NewsFeedService) GetNewsByID(id string) *models.NewsArticle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.articles {
		if a.ID == id || a.Slug == id {
			return &a
		}
	}
	return nil
}

// refineCategoryFromContent uses the feed's default category but overrides
// with a more specific one if content keywords strongly match.
func refineCategoryFromContent(title, summary, feedCategory string) string {
	combined := strings.ToLower(title + " " + summary)

	if strings.Contains(combined, "opec") {
		return "OPEC"
	}
	if strings.Contains(combined, "natural gas") || strings.Contains(combined, " lng ") || strings.Contains(combined, "henry hub") || strings.Contains(combined, "methane") {
		return "Natural Gas"
	}
	if strings.Contains(combined, "refin") || strings.Contains(combined, "gasoline") || strings.Contains(combined, "crack spread") || strings.Contains(combined, "diesel") || strings.Contains(combined, "jet fuel") {
		return "Refining"
	}
	if strings.Contains(combined, "geopolitic") || strings.Contains(combined, "sanction") || strings.Contains(combined, "tariff") || strings.Contains(combined, "conflict") || strings.Contains(combined, "war ") {
		return "International"
	}
	if strings.Contains(combined, "inventor") || strings.Contains(combined, "stockpile") || strings.Contains(combined, "storage") || strings.Contains(combined, " eia ") || strings.Contains(combined, "crude stock") {
		return "Inventory"
	}
	if strings.Contains(combined, "drill") || strings.Contains(combined, "extract") || strings.Contains(combined, "upstream") || strings.Contains(combined, "shale") || strings.Contains(combined, "rig count") || strings.Contains(combined, "permian") || strings.Contains(combined, "offshore") {
		return "Extraction"
	}
	if strings.Contains(combined, "technolog") || strings.Contains(combined, "engineer") || strings.Contains(combined, "innovat") || strings.Contains(combined, "carbon capture") || strings.Contains(combined, "hydrogen") {
		return "Technology"
	}
	if strings.Contains(combined, "demand") || strings.Contains(combined, "consumption") || strings.Contains(combined, "import") {
		return "Demand"
	}
	if strings.Contains(combined, "supply") || strings.Contains(combined, "production") || strings.Contains(combined, "output") {
		return "Supply"
	}

	return feedCategory
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func parseRSSDate(s string) time.Time {
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Now()
}

func hashID(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

func estimateReadTime(text string) string {
	words := len(strings.Fields(text))
	mins := words / 200
	if mins < 1 {
		mins = 1
	}
	return fmt.Sprintf("%d min read", mins)
}
