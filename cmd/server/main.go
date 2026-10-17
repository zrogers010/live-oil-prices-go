package main

import (
	"context"
	"fmt"
	"live-oil-prices-go/internal/assets"
	"live-oil-prices-go/internal/handlers"
	"live-oil-prices-go/internal/middleware"
	"live-oil-prices-go/internal/services"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var commoditySymbols = []string{
	"WTI", "BRENT", "NATGAS", "HEATING", "RBOB",
	"OPEC", "DUBAI", "MURBAN", "WCS", "GASOIL",
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := assets.Init(); err != nil {
		log.Fatalf("Failed to initialize asset hashes: %v", err)
	}

	if err := handlers.InitCommodityTemplate("web/templates/commodity.html"); err != nil {
		log.Fatalf("Failed to parse commodity template: %v", err)
	}

	if err := handlers.InitPageTemplates("web/templates"); err != nil {
		log.Fatalf("Failed to parse page templates: %v", err)
	}

	marketService := services.NewMarketDataService()
	newsService := services.NewNewsFeedService()
	handler := newServerHandler(marketService, newsService)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Server shutdown error: %v", err)
		}
	}()

	fmt.Printf("\n  Live Oil Prices Server\n")
	fmt.Printf("  ──────────────────────\n")
	fmt.Printf("  → http://localhost:%s\n\n", port)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func newServerHandler(market handlers.MarketDataClient, news handlers.NewsClient) http.Handler {
	api := handlers.NewAPI(market, news)

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Server-rendered HTML pages. Each route uses Go 1.22 exact-match
	// patterns so they take precedence over the static FileServer below.
	mux.HandleFunc("GET /{$}", api.ServeHome)
	mux.HandleFunc("GET /charts", api.ServeCharts)
	mux.HandleFunc("GET /forecast", api.ServeForecast)
	mux.HandleFunc("GET /news", api.ServeNews)
	mux.HandleFunc("GET /disclosure", api.ServeDisclosure)
	mux.HandleFunc("GET /privacy", api.ServePrivacy)
	mux.HandleFunc("GET /commodity/{symbol}", api.ServeCommodityPage)

	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		now := time.Now().Format("2006-01-02")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>1.0</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/charts</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/forecast</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/news</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/disclosure</loc><lastmod>%s</lastmod><changefreq>yearly</changefreq><priority>0.3</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/privacy</loc><lastmod>%s</lastmod><changefreq>monthly</changefreq><priority>0.3</priority></url>`, now)
		for _, sym := range commoditySymbols {
			fmt.Fprintf(w, `<url><loc>https://www.liveoilprices.com/commodity/%s</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>0.8</priority></url>`, sym, now)
		}
		fmt.Fprint(w, `</urlset>`)
	})

	// ads.txt - only serve when ADSENSE_CLIENT is set
	mux.HandleFunc("GET /ads.txt", func(w http.ResponseWriter, r *http.Request) {
		client := os.Getenv("ADSENSE_CLIENT")
		if client == "" || !strings.HasPrefix(client, "ca-pub-") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		// Extract pub ID (e.g. ca-pub-1234567890123456 -> 1234567890123456)
		pubID := strings.TrimPrefix(client, "ca-pub-")
		fmt.Fprintf(w, "google.com, pub-%s, DIRECT, f08c47fec0942fa0\n", pubID)
	})

	// Serve static files, but disable directory listings (return 404 for directories)
	fileServer := http.FileServer(http.Dir("web/static"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the path is a directory
		path := filepath.Join("web/static", filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return middleware.Chain(mux)
}
