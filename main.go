package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {

	go startProxiedProxy()

	server := &http.Server{
		Addr: ":8080",
		// Handler: http.HandlerFunc(handleProxy),
		Handler: authenticate(http.HandlerFunc(handleProxy), "haha", "hehe"),
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

// middleware
func authenticate(handler http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Proxy-Authorization")
		if authHeader == "" {
			w.Header().Set("Proxy-Authenticate", "Basic realm=\"Proxy\"")
			http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
			return
		}

		authParts := strings.SplitN(authHeader, " ", 2)
		if len(authParts) != 2 || authParts[0] != "Basic" {
			http.Error(w, "Invalid authentication format", http.StatusBadRequest)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(authParts[1])
		if err != nil {
			http.Error(w, "Invalid authentication token", http.StatusBadRequest)
			return
		}

		creds := strings.SplitN(string(decoded), ":", 2)
		if len(creds) != 2 || creds[0] != username || creds[1] != password {
			http.Error(w, "Invalid username or password", http.StatusForbidden)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
