package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	httpin "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/adapter/in/http"
	archiveout "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/adapter/out/archive"
	converterout "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/adapter/out/converter"
	httpclient "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/adapter/out/httpclient"
	archiveapp "github.com/Viespirkiu-grupe/goviesdezeproxyrewrite/internal/app/archive"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func getenvMust(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s must be set (e.g. in .env)", k)
	}
	return v
}

func cleanTmp() {
	for {
		cmd := exec.Command("find", "/tmp", "-mindepth", "1", "-mmin", "+5", "-exec", "rm", "-rf", "{}", "+")
		if err := cmd.Run(); err != nil {
			log.Println("Error running find:", err)
		}
		time.Sleep(1 * time.Minute)
	}
}

func envDurationSeconds(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(v)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func main() {
	go cleanTmp() // runs in background

	_ = godotenv.Load() // optional: ignore error

	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "4000"
	}
	mainServer := getenvMust("MAIN_SERVER") // e.g. http://localhost:3000
	apiKey := getenvMust("PROXY_API_KEY")   // same as DB
	dialTimeout := envDurationSeconds("PROXY_UPSTREAM_DIAL_TIMEOUT", 10*time.Second)
	headerTimeout := envDurationSeconds("PROXY_UPSTREAM_RESPONSE_HEADER_TIMEOUT", 30*time.Second)
	baseURL, err := url.Parse(mainServer)
	if err != nil {
		log.Fatalf("invalid MAIN_SERVER: %v", err)
	}

	// HTTP client for both info and file requests
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   0, // no global timeout; rely on request context
	}

	infoClient := httpclient.New(baseURL, apiKey, client)
	archiveGateway := archiveout.NewGateway()
	converterGateway := converterout.NewGateway()
	service := archiveapp.NewService(infoClient, infoClient, archiveGateway, converterGateway)
	handlerArchive := httpin.NewHandler(service).HandleArchive

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.StripSlashes)

	r.Get("/{dokId:[0-9]+}/{fileId:[0-9]+}", handlerArchive)
	r.Get("/{dokId:[0-9]+}/{fileId:[0-9]+}/*", handlerArchive)

	r.Get("/{id:[0-9a-fA-F]{32}|[0-9]+}", handlerArchive)
	r.Get("/{id:[0-9a-fA-F]{32}|[0-9]+}/*", handlerArchive)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Printf("Proxy server listening on port %s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("bye.")
}
