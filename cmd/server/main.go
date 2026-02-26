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

	marketService := services.NewMarketDataService()
	newsService := services.NewNewsFeedService()
	api := handlers.NewAPI(marketService, newsService)

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	mux.HandleFunc("GET /commodity/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/commodity.html")
	})

	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		now := time.Now().Format("2006-01-02")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>1.0</priority></url>`, now)
		for _, sym := range commoditySymbols {
			fmt.Fprintf(w, `<url><loc>https://liveoilprices.com/commodity/%s</loc><lastmod>%s</lastmod><changefreq>always</changefreq><priority>0.8</priority></url>`, sym, now)
		}
		fmt.Fprint(w, `</urlset>`)
	})

	staticFS := http.FileServer(http.Dir("web/static"))
	mux.Handle("/", staticFS)

	handler := middleware.Chain(mux)

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
