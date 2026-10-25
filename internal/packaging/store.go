package packaging

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/werewolf-linux/packages/internal/apk"
)

// Store names a repository prefix in the shared immutable object store.
// Clients only commit an index after uploading every object that it references.
type Store struct{ Prefix string }

func (store Store) request(ctx context.Context, operation string, args ...string) ([]byte, error) {
	argv := []string{"--endpoint-url", env("PACKAGES_R2_ENDPOINT", "https://c860af766754fb92eec27bc723e6db5d.r2.cloudflarestorage.com"), "--region", "auto", "s3api", operation, "--bucket", env("PACKAGES_BUCKET", "werewolf")}
	cmd := exec.CommandContext(ctx, "aws", append(argv, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("object store %s: %w: %s", operation, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Fetch reads object bytes and their ETag together. Only a genuine 404 is absent.
func (store Store) Fetch(ctx context.Context, key, destination string) (string, error) {
	data, err := store.request(ctx, "get-object", "--key", store.Prefix+key, destination)
	if err != nil {
		if strings.Contains(err.Error(), "(NoSuchKey)") || strings.Contains(err.Error(), "(404)") {
			return "", nil
		}
		return "", err
	}
	var response struct{ ETag string }
	if err := json.Unmarshal(data, &response); err != nil {
		return "", err
	}
	if response.ETag == "" {
		return "", fmt.Errorf("object store returned no ETag")
	}
	return response.ETag, nil
}

func raced(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "(PreconditionFailed)") || strings.Contains(err.Error(), "(ConditionalRequestConflict)"))
}

// Immutable creates an object once, accepting a retry only for identical bytes.
func (store Store) Immutable(ctx context.Context, path, key string) error {
	directory, err := os.MkdirTemp("", "packages-object-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	previous := filepath.Join(directory, "previous")
	same := func() (bool, error) {
		etag, err := store.Fetch(ctx, key, previous)
		if err != nil || etag == "" {
			return false, err
		}
		a, err := digest(path)
		if err != nil {
			return false, err
		}
		b, err := digest(previous)
		if err != nil {
			return false, err
		}
		if a != b {
			return false, fmt.Errorf("immutable object has different bytes; advance package policy or epoch: %s", key)
		}
		return true, nil
	}
	if found, err := same(); err != nil || found {
		return err
	}
	_, err = store.request(ctx, "put-object", "--key", store.Prefix+key, "--body", path, "--if-none-match", "*", "--cache-control", "public, max-age=31536000, immutable", "--content-type", contentType(key))
	if raced(err) {
		if found, retryErr := same(); retryErr != nil || found {
			return retryErr
		}
	}
	return err
}

func (store Store) conditional(ctx context.Context, key, path, etag string) error {
	args := []string{"--key", store.Prefix + key, "--body", path, "--cache-control", "no-cache", "--content-type", contentType(key)}
	if etag == "" {
		args = append(args, "--if-none-match", "*")
	} else {
		args = append(args, "--if-match", etag)
	}
	_, err := store.request(ctx, "put-object", args...)
	return err
}

func contentType(key string) string {
	switch filepath.Ext(key) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// MergeIndex commits a signed union against the exact index read. Concurrent
// publishers re-read and merge the winning version rather than losing packages.
func (store Store) MergeIndex(ctx context.Context, current map[string]string, arch string, key *rsa.PrivateKey, trust *rsa.PublicKey, work string, keepExisting bool) error {
	previous, next := filepath.Join(work, arch+"-previous-index"), filepath.Join(work, arch+"-next-index")
	remote := arch + "/APKINDEX.tar.gz"
	for attempt := 0; attempt < 12; attempt++ {
		etag, err := store.Fetch(ctx, remote, previous)
		if err != nil {
			return err
		}
		merged := map[string]string{}
		if etag != "" {
			merged, err = apk.Index(previous, arch, trust)
			if err != nil {
				return err
			}
		}
		added := 0
		for name, stanza := range current {
			if old, ok := merged[name]; ok {
				if old != stanza {
					if !keepExisting {
						return fmt.Errorf("published package changed: %s", name)
					}
					slog.Warn("retaining earlier published build; increment policy revision", "package", name)
				}
				continue
			}
			merged[name] = stanza
			added++
		}
		if added == 0 && etag != "" {
			return nil
		}
		data, err := apk.SignedIndex(merged, key)
		if err != nil {
			return err
		}
		if err := writeFile(next, data, 0644); err != nil {
			return err
		}
		if _, err := apk.Index(next, arch, trust); err != nil {
			return err
		}
		err = store.conditional(ctx, remote, next, etag)
		if err == nil {
			slog.Info("committed package index", "repository", store.Prefix, "arch", arch, "added", added)
			return nil
		}
		if !raced(err) {
			return err
		}
		slog.Info("index changed concurrently; merging again", "repository", store.Prefix, "arch", arch, "attempt", attempt+1)
		if err := wait(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("index kept changing: %s%s", store.Prefix, remote)
}

// Envelope authenticates a manifest independently of object storage or HTTPS.
func Envelope(manifest any, key *rsa.PrivateKey) ([]byte, error) {
	payload, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	signature, err := apk.Sign(payload, key)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Format    string `json:"format"`
		Payload   string `json:"payload"`
		Signature string `json:"signature"`
	}{"werewolf-package-release/1", base64.StdEncoding.EncodeToString(payload), base64.StdEncoding.EncodeToString(signature)})
}

// OpenEnvelope verifies the exact payload bytes before decoding its fields.
func OpenEnvelope(data []byte, trust *rsa.PublicKey, value any) error {
	var envelope struct{ Format, Payload, Signature string }
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Format != "werewolf-package-release/1" || strings.ContainsAny(envelope.Payload+envelope.Signature, "\r\n") {
		return fmt.Errorf("invalid signed manifest envelope")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(envelope.Payload)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil {
		return err
	}
	if err := apk.Verify(payload, signature, trust); err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}
