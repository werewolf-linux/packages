// Package plex verifies Plex's signed vendor repository and pinned codec ABI.
package plex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const repository = "https://repo.plex.tv/deb/"
const primary = "6EFFEB478A6559D75C7C4FE706C521790B9CFFDE"

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var filenamePattern = regexp.MustCompile(`^pool/main/p/plexmediaserver/[A-Za-z0-9_.+-]+\.deb$`)
var codecPattern = regexp.MustCompile(`^lib[a-z0-9_]+\.so$`)

// Source is a byte-pinned artifact from the signed repository.
type Source struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Codec struct {
	URL     string `json:"url"`
	FileSHA string `json:"fileSha"`
	SHA256  string `json:"fileSha256"`
	Name    string `json:"fileName"`
	Build   string `json:"build"`
}
type Architecture struct {
	Deb    Source  `json:"deb"`
	Codecs []Codec `json:"codecs"`
}

// Sources retains the vendor and codec identities independently of APK epochs.
type Sources struct {
	Version       string                  `json:"version"`
	CodecSet      string                  `json:"codec_set"`
	Architectures map[string]Architecture `json:"architectures"`
}
type releaseHash struct {
	SHA256 string
	Size   int64
}

func command(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

func signedRelease(ctx context.Context, directory string) (map[string]releaseHash, error) {
	home, err := os.MkdirTemp("", "plex-key-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	base := []string{"gpg", "--homedir", home, "--batch"}
	if _, err := command(ctx, append(base, "--import", filepath.Join(directory, "PlexSign.v2.key"))...); err != nil {
		return nil, err
	}
	keys, err := command(ctx, append(base, "--with-colons", "--fingerprint")...)
	if err != nil {
		return nil, err
	}
	fingerprint := ""
	count := 0
	for _, line := range strings.Split(string(keys), "\n") {
		fields := strings.Split(line, ":")
		if fields[0] == "pub" {
			count++
		}
		if fields[0] == "fpr" && len(fields) > 9 && fingerprint == "" {
			fingerprint = fields[9]
		}
	}
	if count != 1 || fingerprint != primary {
		return nil, fmt.Errorf("unexpected Plex signing key")
	}
	release, err := command(ctx, append(base, "--decrypt", filepath.Join(directory, "InRelease"))...)
	if err != nil {
		return nil, err
	}
	_, section, ok := strings.Cut(string(release), "\nSHA256:\n")
	if !ok {
		return nil, fmt.Errorf("signed release has no SHA256 section")
	}
	hashes := map[string]releaseHash{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, " ") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !hashPattern.MatchString(fields[0]) {
			return nil, fmt.Errorf("malformed signed repository checksum")
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid signed artifact size")
		}
		if _, ok := hashes[fields[2]]; ok {
			return nil, fmt.Errorf("duplicate signed artifact checksum")
		}
		hashes[fields[2]] = releaseHash{SHA256: fields[0], Size: size}
	}
	return hashes, nil
}

func packageFields(data []byte) (map[string]string, error) {
	var found map[string]string
	for _, stanza := range strings.Split(strings.TrimSpace(string(data)), "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(stanza, "\n") {
			key, value, ok := strings.Cut(line, ": ")
			if ok {
				if _, ok := fields[key]; ok {
					return nil, fmt.Errorf("duplicate vendor package field")
				}
				fields[key] = value
			}
		}
		if fields["Package"] != "plexmediaserver" {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple Plex packages in signed index")
		}
		if !filenamePattern.MatchString(fields["Filename"]) || !hashPattern.MatchString(fields["SHA256"]) {
			return nil, fmt.Errorf("unsafe vendor filename or missing checksum")
		}
		found = fields
	}
	if found == nil {
		return nil, fmt.Errorf("Plex package absent from signed index")
	}
	return found, nil
}

// Verify authenticates captured indexes and every vendor and codec recipe pin.
func Verify(ctx context.Context, directory string) (Sources, error) {
	var sources Sources
	data, err := os.ReadFile(filepath.Join(directory, "sources.json"))
	if err != nil {
		return sources, err
	}
	if err := json.Unmarshal(data, &sources); err != nil {
		return sources, err
	}
	if len(sources.Architectures) != 2 || !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(sources.CodecSet) {
		return sources, fmt.Errorf("invalid vendor architecture or codec set")
	}
	hashes, err := signedRelease(ctx, directory)
	if err != nil {
		return sources, err
	}
	recipe, err := os.ReadFile(filepath.Join(directory, "plex.yaml"))
	if err != nil {
		return sources, err
	}
	for arch, apt := range map[string]string{"aarch64": "arm64", "x86_64": "amd64"} {
		raw, err := os.ReadFile(filepath.Join(directory, "Packages."+apt))
		if err != nil {
			return sources, err
		}
		sum := sha256.Sum256(raw)
		expected, ok := hashes["main/binary-"+apt+"/Packages"]
		if !ok || hex.EncodeToString(sum[:]) != expected.SHA256 || int64(len(raw)) != expected.Size {
			return sources, fmt.Errorf("unsigned or altered package index: %s", arch)
		}
		fields, err := packageFields(raw)
		if err != nil {
			return sources, err
		}
		source, ok := sources.Architectures[arch]
		if !ok || fields["Version"] != sources.Version || source.Deb != (Source{URL: repository + fields["Filename"], SHA256: fields["SHA256"]}) {
			return sources, fmt.Errorf("vendor package identity changed: %s", arch)
		}
		entries := []Source{source.Deb}
		for _, codec := range source.Codecs {
			expected := fmt.Sprintf("https://downloads.plex.tv/codecs/%s/linux-%s-standard/%s", sources.CodecSet, arch, codec.Name)
			if codec.URL != expected || !codecPattern.MatchString(codec.Name) {
				return sources, fmt.Errorf("unsafe codec input")
			}
			entries = append(entries, Source{URL: codec.URL, SHA256: codec.SHA256})
		}
		for _, entry := range entries {
			if !hashPattern.MatchString(entry.SHA256) || bytes.Count(recipe, []byte("uri: "+entry.URL+"\n")) != 1 || !bytes.Contains(recipe, []byte("uri: "+entry.URL+"\n      expected-sha256: "+entry.SHA256+"\n")) {
				return sources, fmt.Errorf("missing, duplicate or altered recipe pin: %s", entry.URL)
			}
		}
	}
	return sources, nil
}

func download(ctx context.Context, address, destination string, limit int64) (string, error) {
	if !strings.HasPrefix(address, repository) {
		return "", fmt.Errorf("unexpected vendor download host")
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 10 || !strings.HasPrefix(request.URL.String(), repository) {
			return fmt.Errorf("unexpected vendor redirect")
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "werewolf-packages-update/1")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vendor download: %s", response.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if size > limit {
		return "", fmt.Errorf("oversized vendor artifact")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkCodecABI(ctx context.Context, deb, codecSet string) error {
	members, err := command(ctx, "ar", "t", deb)
	if err != nil {
		return err
	}
	payload := ""
	for _, name := range strings.Fields(string(members)) {
		if strings.HasPrefix(name, "data.tar.") {
			if payload != "" || name != "data.tar.xz" && name != "data.tar.gz" {
				return fmt.Errorf("review changed vendor archive compression")
			}
			payload = name
		}
	}
	if payload == "" {
		return fmt.Errorf("vendor DEB lacks data archive")
	}
	ar := exec.CommandContext(ctx, "ar", "p", deb, payload)
	stream, err := ar.StdoutPipe()
	if err != nil {
		return err
	}
	tar := exec.CommandContext(ctx, "bsdtar", "-xOf", "-", "--include", "./usr/lib/plexmediaserver/Plex Transcoder", "--include", "usr/lib/plexmediaserver/Plex Transcoder")
	tar.Stdin = stream
	var stderr bytes.Buffer
	tar.Stderr = &stderr
	transcoder, err := tar.StdoutPipe()
	if err != nil {
		return err
	}
	if err := ar.Start(); err != nil {
		return err
	}
	if err := tar.Start(); err != nil {
		ar.Process.Kill()
		ar.Wait()
		return err
	}
	needle := []byte(codecSet)
	buffer := make([]byte, 64<<10)
	var tail []byte
	found := false
	var readErr error
	for {
		count, err := transcoder.Read(buffer)
		if count > 0 {
			chunk := append(tail, buffer[:count]...)
			found = found || bytes.Contains(chunk, needle)
			keep := min(len(needle)-1, len(chunk))
			tail = append(tail[:0], chunk[len(chunk)-keep:]...)
		}
		if err != nil {
			if err != io.EOF {
				readErr = err
			}
			break
		}
	}
	tarErr := tar.Wait()
	stream.Close()
	arErr := ar.Wait()
	if readErr != nil {
		return readErr
	}
	if tarErr != nil {
		return fmt.Errorf("vendor archive: %w: %s", tarErr, stderr.String())
	}
	if arErr != nil {
		return arErr
	}
	if !found {
		return fmt.Errorf("Plex codec ABI changed or cannot be verified; review codec manifests before publishing")
	}
	return nil
}

// Refresh prepares a scratch context. The updater commits the whole context
// only after this validation and Melange's recipe validation both succeed.
func Refresh(ctx context.Context, directory, version string) error {
	if !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+-[0-9a-f]+$`).MatchString(version) {
		return fmt.Errorf("invalid vendor release version")
	}
	sources, err := Verify(ctx, directory)
	if err != nil {
		return err
	}
	previous := sources
	if _, err := download(ctx, repository+"dists/public/InRelease", filepath.Join(directory, "InRelease"), 8<<20); err != nil {
		return err
	}
	hashes, err := signedRelease(ctx, directory)
	if err != nil {
		return err
	}
	recipe, err := os.ReadFile(filepath.Join(directory, "plex.yaml"))
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(recipe), sources.Version, version)
	architectures := map[string]Architecture{}
	for arch, apt := range map[string]string{"aarch64": "arm64", "x86_64": "amd64"} {
		name := "main/binary-" + apt + "/Packages"
		path := filepath.Join(directory, "Packages."+apt)
		sum, err := download(ctx, repository+"dists/public/"+name, path, 8<<20)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		expected, ok := hashes[name]
		if !ok || sum != expected.SHA256 || info.Size() != expected.Size {
			return fmt.Errorf("vendor index differs from signed release")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fields, err := packageFields(raw)
		if err != nil {
			return err
		}
		if fields["Version"] != version {
			return fmt.Errorf("native architectures differ from discovered release")
		}
		remote := Source{URL: repository + fields["Filename"], SHA256: fields["SHA256"]}
		temporary, err := os.MkdirTemp("", "plex-deb-")
		if err != nil {
			return err
		}
		deb := filepath.Join(temporary, "plex.deb")
		sum, fetchErr := download(ctx, remote.URL, deb, 1<<30)
		if fetchErr == nil && sum != remote.SHA256 {
			fetchErr = fmt.Errorf("vendor DEB checksum mismatch")
		}
		if fetchErr == nil {
			fetchErr = checkCodecABI(ctx, deb, sources.CodecSet)
		}
		os.RemoveAll(temporary)
		if fetchErr != nil {
			return fetchErr
		}
		native := sources.Architectures[arch]
		text = strings.ReplaceAll(text, native.Deb.URL, remote.URL)
		text = strings.ReplaceAll(text, native.Deb.SHA256, remote.SHA256)
		native.Deb = remote
		architectures[arch] = native
	}
	sources.Version = version
	sources.Architectures = architectures
	baseVersion := strings.SplitN(version, "-", 2)[0]
	epochPattern := regexp.MustCompile(`(?m)^  epoch: ([0-9]+)$`)
	match := epochPattern.FindStringSubmatch(text)
	if match == nil {
		return fmt.Errorf("recipe lacks epoch")
	}
	epoch := 0
	if baseVersion == strings.SplitN(previous.Version, "-", 2)[0] {
		epoch, err = strconv.Atoi(match[1])
		if err != nil {
			return err
		}
		epoch++
	}
	text = regexp.MustCompile(`(?m)^  version: .*$`).ReplaceAllString(text, "  version: "+baseVersion)
	text = epochPattern.ReplaceAllString(text, "  epoch: "+strconv.Itoa(epoch))
	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "sources.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "plex.yaml"), []byte(text), 0644); err != nil {
		return err
	}
	_, err = Verify(ctx, directory)
	return err
}
