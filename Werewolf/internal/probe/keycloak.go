package probe

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Keycloak exercises administration, OIDC and SAML against a disposable database.
func Keycloak(ctx context.Context) error {
	if err := keycloakProviderMtimes("/usr/lib/keycloak"); err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "keycloak-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "keycloak")
	if err := os.Mkdir(home, 0755); err != nil {
		return err
	}
	for _, name := range []string{"conf", "lib", "providers", "themes"} {
		if err := os.Symlink(filepath.Join("/usr/lib/keycloak", name), filepath.Join(home, name)); err != nil {
			return err
		}
	}
	if err := os.Mkdir(filepath.Join(home, "data"), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile("/usr/lib/keycloak/java.args")
	if err != nil {
		return err
	}
	if strings.Count(string(data), "-Dkc.home.dir=/usr/lib/keycloak") != 1 {
		return fmt.Errorf("Keycloak home argument changed")
	}
	args := filepath.Join(root, "java.args")
	if err := os.WriteFile(args, []byte(strings.ReplaceAll(string(data), "-Dkc.home.dir=/usr/lib/keycloak", "-Dkc.home.dir="+home)), 0600); err != nil {
		return err
	}
	password := filepath.Join(root, "password")
	if err := os.WriteFile(password, []byte("consumer-db-password"), 0600); err != nil {
		return err
	}
	pg := filepath.Join(root, "pg")
	if _, err := checked(ctx, "initdb", "-D", pg, "-U", "keycloak", "--pwfile="+password, "--auth-local=trust", "--auth-host=scram-sha-256"); err != nil {
		return err
	}
	if _, err := checked(ctx, "pg_ctl", "-D", pg, "-l", filepath.Join(root, "pg.log"), "-o", "-h 127.0.0.1 -k "+root, "-w", "start"); err != nil {
		return err
	}
	defer func() {
		// Cleanup must still run after the caller cancels the probe.
		command := exec.Command("pg_ctl", "-D", pg, "-m", "immediate", "-t", "10", "-w", "stop")
		if output, err := command.CombinedOutput(); err != nil {
			slog.Error("stop probe database", "error", err, "output", string(output))
		}
	}()
	log, err := os.Create(filepath.Join(root, "keycloak.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.CommandContext(ctx, "java", "-Xmx768m", "@"+args, "start", "--optimized")
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		values[key] = value
	}
	for key, value := range map[string]string{
		"KC_DB_URL": "jdbc:postgresql://127.0.0.1:5432/postgres", "KC_DB_USERNAME": "keycloak", "KC_DB_PASSWORD": "consumer-db-password",
		"KC_HTTP_ENABLED": "true", "KC_HTTP_PORT": "18080", "KC_HOSTNAME": "http://localhost:18080", "KC_BOOTSTRAP_ADMIN_USERNAME": "admin", "KC_BOOTSTRAP_ADMIN_PASSWORD": "consumer-admin-password",
	} {
		values[key] = value
	}
	command.Env = environment(values)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	exited := false
	defer func() {
		if exited {
			return
		}
		command.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			command.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	const base = "http://127.0.0.1:18080"
	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		_, status, err := keycloakRequest(ctx, client, base+"/realms/master/.well-known/openid-configuration", nil, "", false)
		if err == nil && status == http.StatusOK {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case exitErr := <-done:
			exited = true
			return keycloakFailure(log, fmt.Errorf("Keycloak exited before readiness: %v", exitErr))
		case <-deadline.C:
			return keycloakFailure(log, fmt.Errorf("Keycloak readiness timed out"))
		case <-tick.C:
		}
	}
	return keycloakFlows(ctx, client, base)
}

// An optimized server records provider mtimes during kc.sh build. Check the
// installed APK before starting services so packaging drift identifies the JAR.
func keycloakProviderMtimes(installed string) error {
	archive, err := zip.OpenReader(filepath.Join(installed, "lib/quarkus/generated-bytecode.jar"))
	if err != nil {
		return err
	}
	defer archive.Close()
	properties, err := archive.Open("META-INF/keycloak-persisted.properties")
	if err != nil {
		return err
	}
	defer properties.Close()
	recorded := map[string]string{}
	scanner := bufio.NewScanner(properties)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && strings.HasPrefix(key, "kc.provider.file.") {
			recorded[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	providers, err := filepath.Glob(filepath.Join(installed, "providers/*.jar"))
	if err != nil {
		return err
	}
	for _, provider := range providers {
		info, err := os.Stat(provider)
		if err != nil {
			return err
		}
		name := filepath.Base(provider)
		build := recorded["kc.provider.file."+name+".last-modified"]
		actual := fmt.Sprint(info.ModTime().UnixMilli())
		if build != actual {
			return fmt.Errorf("%s: build mtime=%s, installed mtime=%s", name, build, actual)
		}
	}
	return nil
}

func keycloakFailure(log *os.File, failure error) error {
	data, err := io.ReadAll(io.NewSectionReader(log, 0, 1<<20))
	if err != nil {
		return fmt.Errorf("%w; read server log: %v", failure, err)
	}
	return fmt.Errorf("%w: %s", failure, data)
}

func keycloakRequest(ctx context.Context, client *http.Client, endpoint string, payload map[string]any, token string, form bool) ([]byte, int, error) {
	method := http.MethodGet
	var data []byte
	contentType := "application/json"
	if payload != nil {
		method = http.MethodPost
		if form {
			values := url.Values{}
			for key, value := range payload {
				values.Set(key, fmt.Sprint(value))
			}
			data = []byte(values.Encode())
			contentType = "application/x-www-form-urlencoded"
		} else {
			var err error
			data, err = json.Marshal(payload)
			if err != nil {
				return nil, 0, err
			}
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(body) > 4<<20 {
		return nil, response.StatusCode, fmt.Errorf("oversized Keycloak response")
	}
	return body, response.StatusCode, nil
}

func keycloakFlows(ctx context.Context, client *http.Client, base string) error {
	request := func(path string, payload map[string]any, token string, form bool) ([]byte, error) {
		data, status, err := keycloakRequest(ctx, client, base+path, payload, token, form)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("Keycloak %s: HTTP %d", path, status)
		}
		return data, nil
	}
	login, err := request("/realms/master/protocol/openid-connect/token", map[string]any{"grant_type": "password", "client_id": "admin-cli", "username": "admin", "password": "consumer-admin-password"}, "", true)
	if err != nil {
		return err
	}
	var credentials struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(login, &credentials); err != nil {
		return err
	}
	if credentials.AccessToken == "" {
		return fmt.Errorf("missing admin access token")
	}
	if _, err := request("/admin/realms", map[string]any{"realm": "consumer", "enabled": true}, credentials.AccessToken, false); err != nil {
		return err
	}
	if _, err := request("/admin/realms/consumer/clients", map[string]any{"clientId": "consumer", "enabled": true, "publicClient": false, "secret": "consumer-client-secret", "serviceAccountsEnabled": true}, credentials.AccessToken, false); err != nil {
		return err
	}
	const realm = "/realms/consumer"
	discovery, err := request(realm+"/.well-known/openid-configuration", nil, "", false)
	if err != nil {
		return err
	}
	var config struct {
		Methods []string `json:"code_challenge_methods_supported"`
	}
	if err := json.Unmarshal(discovery, &config); err != nil {
		return err
	}
	found := false
	for _, method := range config.Methods {
		found = found || method == "S256"
	}
	if !found {
		return fmt.Errorf("Keycloak PKCE S256 is unavailable")
	}
	grant := map[string]any{"grant_type": "client_credentials", "client_id": "consumer", "client_secret": "consumer-client-secret"}
	result, err := request(realm+"/protocol/openid-connect/token", grant, "", true)
	if err != nil {
		return err
	}
	var response struct {
		Token string `json:"access_token"`
		Type  string `json:"token_type"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return err
	}
	parts := strings.Split(response.Token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" || !strings.EqualFold(response.Type, "bearer") {
		return fmt.Errorf("invalid client bearer token")
	}
	grant["client_secret"] = "wrong"
	_, status, err := keycloakRequest(ctx, client, base+realm+"/protocol/openid-connect/token", grant, "", true)
	if err != nil {
		return err
	}
	if status != http.StatusUnauthorized {
		return fmt.Errorf("wrong client secret returned HTTP %d", status)
	}
	saml, err := request(realm+"/protocol/saml/descriptor", nil, "", false)
	if err != nil {
		return err
	}
	if !bytes.Contains(saml, []byte("EntityDescriptor")) {
		return fmt.Errorf("missing SAML entity descriptor")
	}
	slog.Info("Keycloak consumer flows passed", "checks", []string{"admin", "realm", "client", "OIDC", "PKCE", "SAML", "wrong-secret"})
	return nil
}
