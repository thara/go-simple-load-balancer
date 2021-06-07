package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	Attempts int = iota
	Retry
)

var pool ServerPool

func healthCheck() {
	t := time.NewTicker(time.Second * 20)
	for {
		select {
		case <-t.C:
			log.Println("Starting health check...")
			pool.HealthCheck()
			log.Println("Health check completed")
		}

	}
}

func loadBalance(w http.ResponseWriter, r *http.Request) {
	attempts := getAttemptsFromContext(r)
	if 3 < attempts {
		log.Printf("%s(%s) Max attempts reached, terminating\n", r.RemoteAddr, r.URL.Path)
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
		return
	}

	peer := pool.getNextPeer()
	if peer != nil {
		peer.ReverseProxy.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}

func getRetryFromContext(r *http.Request) int {
	if retry, ok := r.Context().Value(Retry).(int); ok {
		return retry
	}
	return 0
}

func getAttemptsFromContext(r *http.Request) int {
	if retry, ok := r.Context().Value(Attempts).(int); ok {
		return retry
	}
	return 0
}

func main() {
	var backendList string
	var port int
	flag.StringVar(&backendList, "backends", "", "Load balanced backends, use commas to separate")
	flag.IntVar(&port, "port", 3030, "Port to serve")
	flag.Parse()

	if len(backendList) == 0 {
		log.Fatal("Please provide one or more backends to load balance")
	}

	backends := strings.Split(backendList, ",")
	for _, s := range backends {
		backendUrl, err := url.Parse(s)
		if err != nil {
			log.Fatal(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(backendUrl)

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
			log.Printf("[%s] %s\n", backendUrl.Host, e.Error())
			retries := getRetryFromContext(r)
			if retries < 3 {
				select {
				case <-time.After(10 * time.Millisecond):
					ctx := context.WithValue(r.Context(), Retry, retries+1)
					proxy.ServeHTTP(w, r.WithContext(ctx))
				}
				return
			}

			pool.markBackendStatus(backendUrl, false)

			attempts := getAttemptsFromContext(r)
			log.Printf("%s(%s) Attempting retry %d\n", r.RemoteAddr, r.URL.Path, attempts)
			ctx := context.WithValue(r.Context(), Attempts, attempts+1)
			loadBalance(w, r.WithContext(ctx))
		}

		pool.addBackend(&Backend{
			URL:          backendUrl,
			Alive:        true,
			ReverseProxy: proxy,
		})
		log.Printf("Configured server: %s\n", backendUrl)
	}

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: http.HandlerFunc(loadBalance),
	}

	go healthCheck()

	log.Printf("Load Balancer started at :%d\n", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
