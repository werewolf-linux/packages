// Package probe exercises installed packages with local, controlled fixtures.
package probe

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"debug/elf"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// CurlOptions describes the intentionally small supported curl feature set.
type CurlOptions struct {
	Binary        string `yaml:"binary"`
	HTTP2         bool   `yaml:"http2,omitempty"`
	HTTP3         bool   `yaml:"http3,omitempty"`
	PSL           bool   `yaml:"psl,omitempty"`
	Cookies       bool   `yaml:"cookies,omitempty"`
	NetrcDisabled bool   `yaml:"netrc-disabled,omitempty"`
	Musl          bool   `yaml:"musl,omitempty"`
}

// Curl tests the real CLI against local HTTP, TLS, redirects and encodings.
func Curl(ctx context.Context, options CurlOptions) error {
	if options.Binary == "" {
		return fmt.Errorf("curl probe requires a binary")
	}
	var environment []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !slices.Contains([]string{"http_proxy", "https_proxy", "all_proxy", "no_proxy", "curl_ca_bundle", "ssl_cert_file", "ssl_cert_dir"}, strings.ToLower(key)) {
			environment = append(environment, value)
		}
	}
	run := func(expected int, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, options.Binary, append([]string{"-q", "-sS", "--max-time", "10"}, args...)...)
		command.Env = environment
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		code := 0
		if err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				return nil, err
			}
			code = exit.ExitCode()
		}
		if code != expected {
			return nil, fmt.Errorf("curl %v exited %d, expected %d: %s", args, code, expected, stderr.String())
		}
		return stdout.Bytes(), nil
	}
	version, err := run(0, "--version")
	if err != nil {
		return err
	}
	fields := map[string][]string{}
	for _, line := range strings.Split(string(version), "\n") {
		name, value, ok := strings.Cut(line, ": ")
		if ok {
			fields[name] = strings.Fields(value)
		}
	}
	protocols := slices.Clone(fields["Protocols"])
	slices.Sort(protocols)
	if !slices.Equal(protocols, []string{"http", "https"}) {
		return fmt.Errorf("unexpected curl protocols: %s", version)
	}
	features := fields["Features"]
	for _, feature := range []string{"SSL", "IPv6", "libz", "AsynchDNS"} {
		if !slices.Contains(features, feature) {
			return fmt.Errorf("curl lost %s: %s", feature, version)
		}
	}
	if slices.Contains(features, "PSL") != options.PSL {
		return fmt.Errorf("curl PSL policy differs: %s", version)
	}
	for feature, enabled := range map[string]bool{"HTTP2": options.HTTP2, "HTTP3": options.HTTP3} {
		if slices.Contains(features, feature) != enabled {
			return fmt.Errorf("curl %s policy differs: %s", feature, version)
		}
	}
	for _, feature := range []string{"brotli", "zstd", "GSS-API", "Kerberos", "SPNEGO", "NTLM", "UnixSockets", "HTTPS-proxy", "alt-svc", "HSTS"} {
		if slices.Contains(features, feature) {
			return fmt.Errorf("unexpected curl feature %s", feature)
		}
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := []byte("hello")
		status := http.StatusOK
		if request.Method == http.MethodPost || request.Method == http.MethodPut {
			var err error
			body, err = io.ReadAll(io.LimitReader(request.Body, 1<<20))
			if err != nil {
				http.Error(writer, err.Error(), 500)
				return
			}
		} else {
			switch request.URL.Path {
			case "/redirect":
				status = http.StatusFound
				writer.Header().Set("Location", "/hello")
			case "/forbidden":
				status = http.StatusFound
				writer.Header().Set("Location", "file:///etc/passwd")
			case "/gzip":
				var buffer bytes.Buffer
				compressed := gzip.NewWriter(&buffer)
				compressed.Write(body)
				compressed.Close()
				body = buffer.Bytes()
				writer.Header().Set("Content-Encoding", "gzip")
			case "/br", "/zstd":
				writer.Header().Set("Content-Encoding", request.URL.Path[1:])
			case "/cookie":
				writer.Header().Set("Set-Cookie", "session=ok; Path=/")
			case "/check-cookie":
				body = []byte(request.Header.Get("Cookie"))
			case "/auth":
				if request.Header.Get("Authorization") != "Basic dXNlcjpwYXNz" {
					status = http.StatusUnauthorized
					writer.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				}
			case "/etag":
				writer.Header().Set("ETag", `"one"`)
				if request.Header.Get("If-None-Match") == `"one"` {
					status = http.StatusNotModified
					body = nil
				}
			}
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
		writer.WriteHeader(status)
		writer.Write(body)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	directory, err := os.MkdirTemp("", "curl-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		return err
	}
	certPath := filepath.Join(directory, "cert.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0644); err != nil {
		return err
	}
	secure := httptest.NewUnstartedServer(handler)
	secure.EnableHTTP2 = options.HTTP2
	secure.Config.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug)
	secure.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{certificate}, PrivateKey: private}}, MinVersion: tls.VersionTLS12}
	secure.StartTLS()
	defer secure.Close()
	_, port, err := net.SplitHostPort(secure.Listener.Addr().String())
	if err != nil {
		return err
	}
	https := "https://localhost:" + port
	type check struct {
		args    []string
		code    int
		body    string
		compare bool
	}
	checks := []check{
		{[]string{server.URL + "/hello"}, 0, "hello", true},
		{[]string{"-L", server.URL + "/redirect"}, 0, "hello", true},
		{[]string{"-L", server.URL + "/forbidden"}, 1, "", false},
		{[]string{"--compressed", server.URL + "/gzip"}, 0, "hello", true},
		{[]string{"--compressed", server.URL + "/br"}, 61, "", false},
		{[]string{"--compressed", server.URL + "/zstd"}, 61, "", false},
		{[]string{"-d", "posted", server.URL + "/post"}, 0, "posted", true},
		{[]string{"-f", "-u", "user:pass", server.URL + "/auth"}, 0, "hello", true},
		{[]string{https + "/hello"}, 60, "", false},
		{[]string{"--cacert", certPath, "--resolve", "localhost:" + port + ":127.0.0.1", https + "/hello"}, 0, "hello", true},
		{[]string{"--cacert", certPath, secure.URL + "/hello"}, 60, "", false},
	}
	if options.HTTP2 {
		checks = append(checks, check{[]string{"--http2-prior-knowledge", "--cacert", certPath,
			"--resolve", "localhost:" + port + ":127.0.0.1", "-w", "\n%{http_version}", https + "/hello"}, 0, "hello\n2", true})
	}
	if options.HTTP3 {
		packet, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer packet.Close()
		quicServer := &http3.Server{Handler: handler, TLSConfig: http3.ConfigureTLSConfig(secure.TLS.Clone())}
		served := make(chan error, 1)
		go func() { served <- quicServer.Serve(packet) }()
		defer func() {
			quicServer.Close()
			<-served
		}()
		_, quicPort, err := net.SplitHostPort(packet.LocalAddr().String())
		if err != nil {
			return err
		}
		quicURL := "https://localhost:" + quicPort + "/hello"
		resolve := "localhost:" + quicPort + ":127.0.0.1"
		checks = append(checks,
			check{[]string{"--http3-only", "--cacert", certPath, "--resolve", resolve, "-w", "\n%{http_version}", quicURL}, 0, "hello\n3", true},
			check{[]string{"--http3-only", "--resolve", resolve, quicURL}, 60, "", false})
	}
	upload := filepath.Join(directory, "upload")
	if err := os.WriteFile(upload, []byte("uploaded"), 0644); err != nil {
		return err
	}
	checks = append(checks, check{[]string{"-T", upload, server.URL + "/put"}, 0, "uploaded", true})
	if options.NetrcDisabled {
		path := filepath.Join(directory, "netrc")
		if err := os.WriteFile(path, []byte("machine 127.0.0.1 login user password pass\n"), 0600); err != nil {
			return err
		}
		checks = append(checks, check{[]string{"-f", "--netrc", "--netrc-file", path, server.URL + "/auth"}, 22, "", false})
	}
	if options.Cookies {
		jar := filepath.Join(directory, "cookies")
		checks = append(checks, check{[]string{"-c", jar, server.URL + "/cookie"}, 0, "", false}, check{[]string{"-b", jar, server.URL + "/check-cookie"}, 0, "session=ok", true})
	}
	tag := filepath.Join(directory, "etag")
	checks = append(checks, check{[]string{"--etag-save", tag, server.URL + "/etag"}, 0, "", false}, check{[]string{"--etag-compare", tag, "-w", "%{http_code}", server.URL + "/etag"}, 0, "304", true})
	for _, scheme := range []string{"file", "ftp", "ldap", "smtp", "sftp", "ws"} {
		checks = append(checks, check{[]string{scheme + "://127.0.0.1/unavailable"}, 1, "", false})
	}
	for _, check := range checks {
		data, err := run(check.code, check.args...)
		if err != nil {
			return err
		}
		if check.compare && string(data) != check.body {
			return fmt.Errorf("curl %v returned %q, expected %q", check.args, data, check.body)
		}
	}
	if options.Musl {
		if err := CurlLinkage(options.Binary); err != nil {
			return err
		}
	}
	slog.Info("curl probe passed", "binary", options.Binary, "checks", len(checks), "psl", options.PSL)
	return nil
}

// CurlLinkage checks the private Alpine binary without invoking readelf.
func CurlLinkage(path string) error {
	file, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	needed, err := file.ImportedLibraries()
	if err != nil {
		return err
	}
	if len(needed) != 4 {
		return fmt.Errorf("unexpected private curl dependencies: %v", needed)
	}
	for _, library := range []string{"libssl.so.3", "libcrypto.so.3", "libz.so.1"} {
		if !slices.Contains(needed, library) {
			return fmt.Errorf("private curl lost %s", library)
		}
	}
	found := false
	for _, library := range needed {
		found = found || regexp.MustCompile(`^libc\.musl-(aarch64|x86_64)\.so\.1$`).MatchString(library)
	}
	if !found {
		return fmt.Errorf("private curl uses another libc: %v", needed)
	}
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP {
			data, err := io.ReadAll(program.Open())
			if err != nil {
				return err
			}
			if !regexp.MustCompile(`^/lib/ld-musl-(aarch64|x86_64)\.so\.1\x00$`).Match(data) {
				return fmt.Errorf("unexpected private curl interpreter")
			}
			return nil
		}
	}
	return fmt.Errorf("private curl has no program interpreter")
}
