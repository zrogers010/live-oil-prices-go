package main

import (
	"context"
	"fmt"
	"live-oil-prices-go/internal/handlers"
	"live-oil-prices-go/internal/middleware"
	"live-oil-prices-go/internal/services"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	mux.HandleFunc("GET /commodity/{symbol}", api.ServeCommodityPage)

	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		now := time.Now().Format("2006-01-02")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>1.0</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/charts</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/forecast</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/news</loc><lastmod>%s</lastmod><changefreq>hourly</changefreq><priority>0.9</priority></url>`, now)
		for _, sym := range commoditySymbols {
			fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/commodity/%s</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>0.8</priority></url>`, sym, now)
		}
		fmt.Fprint(w, `</urlset>`)
	})

	mux.Handle("/", http.FileServer(http.Dir("web/static")))

	return middleware.Chain(mux)
}
