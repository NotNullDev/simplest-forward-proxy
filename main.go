package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {
	server := &http.Server{
		Addr:    ":8080",
		Handler: http.HandlerFunc(handleProxy),
	}
	fmt.Println("Starting proxy server on :8080")
	server.ListenAndServe()
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		handleTunneling(w, r)
	} else {
		handleHTTP(w, r)
	}
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {

	if isRedditURL(r.URL.String()) {
		http.Error(w, "Access to reddit.com is not allowed.", http.StatusBadRequest)
		return
	}

	fmt.Printf("Accessing: %s\n", r.URL.String())

	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleTunneling(w http.ResponseWriter, r *http.Request) {

	if isRedditURL(r.Host) {
		http.Error(w, "Access to reddit.com is not allowed.", http.StatusBadRequest)
		return
	}

	fmt.Printf("Accessing: %s\n", r.Host)

	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer destConn.Close()

	w.WriteHeader(http.StatusOK)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go transfer(destConn, clientConn, &wg)
	go transfer(clientConn, destConn, &wg)
	wg.Wait()
}

func isRedditURL(url string) bool {
	return strings.Contains(url, "reddit.com") || strings.Contains(url, "www.reddit.com")
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func transfer(dst io.WriteCloser, src io.ReadCloser, wg *sync.WaitGroup) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
	wg.Done()
}
