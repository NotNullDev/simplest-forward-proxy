package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
)

const (
	upstreamProxy = "localhost:8080"
	username      = "haha"
	password      = "hehe"
)

func startProxiedProxy() {
	server := &http.Server{
		Addr:    ":5555",
		Handler: http.HandlerFunc(handleProxyProxied),
	}
	fmt.Println("Proxied proxy started on port 5555")
	server.ListenAndServe()
}

func handleProxyProxied(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		handleTunnelingProxied(w, r)
	} else {
		handleHTTPProxied(w, r)
	}
}

func handleHTTPProxied(w http.ResponseWriter, r *http.Request) {
	// Check if the target URL is https://reddit.com and return a 400 status code.
	if isRedditURL(r.URL.String()) {
		http.Error(w, "Access to reddit.com is not allowed.", http.StatusBadRequest)
		return
	}

	// Print the URL being accessed.
	fmt.Printf("[PROXIED PROXY] http Accessing: %s\n", r.URL.String())

	// Create a custom transport to forward requests to the upstream proxy
	proxyURL, _ := url.Parse(upstreamProxy)
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	// Add authentication to the request if required
	if username != "" && password != "" {
		auth := username + ":" + password
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
		r.Header.Add("Proxy-Authorization", "Basic "+encodedAuth)
	}

	resp, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleTunnelingProxied(w http.ResponseWriter, r *http.Request) {
	// Check if the target URL is https://reddit.com and return a 400 status code.
	if isRedditURL(r.Host) {
		http.Error(w, "Access to reddit.com is not allowed.", http.StatusBadRequest)
		return
	}

	// Print the URL being accessed.
	fmt.Printf("[PROXIED PROXY] Accessing: %s\n", r.Host)

	// Connect to the upstream proxy
	upstreamProxyConn, err := net.Dial("tcp", upstreamProxy)
	if err != nil {
		println(err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer upstreamProxyConn.Close()

	// Send the CONNECT request to the upstream proxy
	connectRequest := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", r.Host, r.Host)

	// Add authentication to the CONNECT request if required
	if username != "" && password != "" {
		auth := username + ":" + password
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
		connectRequest += "Proxy-Authorization: Basic " + encodedAuth + "\r\n"
	}

	fmt.Fprintf(upstreamProxyConn, connectRequest+"\r\n")

	// Read the response from the upstream proxy
	resp, err := http.ReadResponse(bufio.NewReader(upstreamProxyConn), r)
	// Read the response from the upstream proxy
	if err != nil || resp.StatusCode != 200 {
		http.Error(w, "Error connecting to the upstream proxy", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		println(err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	clientConn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(upstreamProxyConn, clientConn)
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, upstreamProxyConn)
	}()

	wg.Wait()
}
