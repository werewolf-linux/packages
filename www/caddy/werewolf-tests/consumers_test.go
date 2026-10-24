//go:build werewolf_consumers

package consumers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func TestCommonConsumers(t *testing.T) {
	binary, err := filepath.Abs("../caddy")
	if err != nil {
		t.Fatal(err)
	}
	modules, err := exec.Command(binary, "list-modules").CombinedOutput()
	if err != nil {
		t.Fatalf("modules: %v: %s", err, modules)
	}
	for _, omitted := range []string{"http.handlers.acme_server", "http.handlers.tracing", "http.encoders.br"} {
		if strings.Contains(string(modules), omitted) {
			t.Fatalf("module remains: %s", omitted)
		}
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy" || r.URL.Query().Get("q") != "test" {
			http.Error(w, "proxy request changed", 400)
			return
		}
		fmt.Fprint(w, "reverse proxy works")
	}))
	defer backend.Close()
	fastcgi, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fastcgi.Close()
	go fcgi.Serve(fastcgi, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 400)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fmt.Fprintf(w, "FastCGI: %s", data)
	}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("static files work"), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{
	admin off
	skip_install_trust
	auto_https disable_redirects
	servers {
		protocols h1 h2 h3
	}
}
https://%s {
	tls internal
	root * %s
	reverse_proxy /proxy %s
	reverse_proxy /fastcgi %s {
		transport fastcgi
	}
	file_server browse
}
`, address, work, backend.URL, fastcgi.Addr().String())
	configPath := filepath.Join(work, "Caddyfile")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(work, "caddy.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(binary, "run", "--config", configPath, "--adapter", "caddyfile")
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+work, "XDG_CONFIG_HOME="+work, "HOME="+work)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	defer func() {
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Log(string(data))
		}
	}()
	pool := x509.NewCertPool()
	var cert []byte
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		cert, err = os.ReadFile(filepath.Join(work, "caddy/pki/authorities/local/root.crt"))
		if err == nil && pool.AppendCertsFromPEM(cert) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(cert) == 0 {
		t.Fatal("internal CA did not issue a root")
	}
	tlsConfig := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	transport := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	base := "https://" + address
	for deadline := time.Now().Add(30 * time.Second); ; {
		response, err := client.Get(base + "/hello.txt")
		if err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	check := func(client *http.Client, method, path, body, expected string, protocol int) {
		t.Helper()
		request, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != 200 || string(data) != expected || response.ProtoMajor != protocol {
			t.Fatalf("%s: status=%d protocol=%s body=%q error=%v", path, response.StatusCode, response.Proto, data, err)
		}
	}
	check(client, "GET", "/hello.txt", "", "static files work", 2)
	listing, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	html, err := io.ReadAll(listing.Body)
	listing.Body.Close()
	if err != nil || listing.StatusCode != 200 || !strings.Contains(string(html), "hello.txt") {
		t.Fatalf("directory browsing: status=%d error=%v", listing.StatusCode, err)
	}
	check(client, "GET", "/proxy?q=test", "", "reverse proxy works", 2)
	check(client, "POST", "/fastcgi", "upload=example", "FastCGI: upload=example", 2)
	quic := &http3.Transport{TLSClientConfig: tlsConfig.Clone()}
	defer quic.Close()
	http3Client := &http.Client{Transport: quic, Timeout: 10 * time.Second}
	check(http3Client, "GET", "/hello.txt", "", "static files work", 3)
	check(http3Client, "GET", "/proxy?q=test", "", "reverse proxy works", 3)
}
