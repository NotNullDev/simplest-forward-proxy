package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
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

	// Custom JSON response for reddit.com
	if isRedditURL(r.Host) {
		w.WriteHeader(http.StatusOK)

		log.Printf("Generating self-signed certificate for %s", r.Host)
		cert, err := generateSelfSignedCertificate("localhost")
		if err != nil {
			log.Printf("Failed to generate self-signed certificate: %v", err)
			return
		}

		clientConn := clientConnection(w, r)
		defer clientConn.Close()

		log.Printf("Starting TLS handshake with %s", r.Host)

		config := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		tlsClientConn := tls.Server(clientConn, config)
		defer tlsClientConn.Close()

		if err := tlsClientConn.Handshake(); err != nil {
			log.Printf("Error during TLS handshake: %v", err)
			return
		}

		statusCode := 400
		jsonBytes, err := createDynamicJSON(statusCode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("Sending JSON response to %s", r.Host)
		tlsClientConn.Write([]byte(fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n", statusCodeString(statusCode))))
		tlsClientConn.Write(jsonBytes)

		tlsClientConn.Close()

		log.Printf("Finished TLS handshake with %s", r.Host)

		return
	}

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

func createDynamicJSON(statusCode int) ([]byte, error) {
	data := map[string]interface{}{
		"status":  statusCode,
		"message": "Access to reddit.com is not allowed.",
	}
	return json.Marshal(data)
}

func statusCodeString(statusCode int) string {
	return fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))
}

func generateSelfSignedCertificate(commonName string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}

	return cert, nil
}

func clientConnection(w http.ResponseWriter, r *http.Request) net.Conn {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return nil
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return nil
	}
	return clientConn
}
