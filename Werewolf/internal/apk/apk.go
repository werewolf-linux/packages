// Package apk validates and signs APK v2 packages and indexes. Payloads are
// streamed; only bounded control metadata is held in memory.
package apk

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const KeyName = "werewolf-packages.rsa.pub"
const controlLimit = 16 << 20

var indexedName = regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`)
var indexedVersion = regexp.MustCompile(`^[A-Za-z0-9_.+~-]+$`)

// Info identifies a validated APK and the exact compressed control member.
type Info struct {
	Name          string
	Version       string
	Arch          string
	Control       []byte
	PayloadOffset int64
	Size          int64
}

// PublicKey reads an RSA PKIX public key.
func PublicKey(data []byte) (*rsa.PublicKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid PEM public key")
	}
	value, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := value.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("APK trust root must be an RSA key of at least 2048 bits")
	}
	return key, nil
}

// PrivateKey accepts an unencrypted PKCS#1 or PKCS#8 PEM key.
func PrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid PEM private key")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		var err error
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
	case "PRIVATE KEY":
		value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		var ok bool
		key, ok = value.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("signing key is not RSA")
		}
	default:
		return nil, fmt.Errorf("unsupported signing key format")
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("signing key is smaller than 2048 bits")
	}
	return key, nil
}

// Sign signs bytes with the APK RSA256 algorithm.
func Sign(data []byte, key *rsa.PrivateKey) ([]byte, error) {
	sum := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
}

// Verify checks a signature before its content is trusted.
func Verify(data, signature []byte, key *rsa.PublicKey) error {
	sum := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], signature)
}

func member(file *os.File, reader *bufio.Reader) ([]byte, []byte, error) {
	start, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, nil, err
	}
	start -= int64(reader.Buffered())
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return nil, nil, err
	}
	gz.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(gz, controlLimit+1))
	closeErr := gz.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	if len(raw) > controlLimit {
		return nil, nil, fmt.Errorf("oversized gzip control member")
	}
	end, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, nil, err
	}
	end -= int64(reader.Buffered())
	if end-start > controlLimit {
		return nil, nil, fmt.Errorf("oversized compressed control member")
	}
	compressed := make([]byte, end-start)
	if _, err := file.ReadAt(compressed, start); err != nil {
		return nil, nil, err
	}
	return compressed, raw, nil
}

func contents(raw []byte) (map[string][]byte, error) {
	found := map[string][]byte{}
	reader := tar.NewReader(bytes.NewReader(raw))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		if _, ok := found[name]; ok {
			return nil, fmt.Errorf("duplicate tar member %s", name)
		}
		if header.Size < 0 || header.Size > controlLimit {
			return nil, fmt.Errorf("oversized tar member")
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		found[name] = data
	}
}

func signature(file *os.File, reader *bufio.Reader) ([]byte, error) {
	_, raw, err := member(file, reader)
	if err != nil {
		return nil, err
	}
	files, err := contents(raw)
	if err != nil {
		return nil, err
	}
	value, ok := files[".SIGN.RSA256."+KeyName]
	if !ok || len(files) != 1 {
		return nil, fmt.Errorf("unexpected APK signing key")
	}
	return value, nil
}

// Check validates identity, signature when requested, and the compressed payload
// hash. A nil trust key is accepted only for unsigned builder artifacts.
func Check(filename, name, version, arch string, trust *rsa.PublicKey) (Info, error) {
	return check(filename, name, version, arch, trust, false)
}

// Inspect validates an APK's structure and payload hash without establishing
// trust. It is for offline comparisons of explicitly selected packages only.
func Inspect(filename string) (Info, error) {
	return check(filename, "", "", "", nil, true)
}

// ControlFiles returns the bounded control metadata from a checked package.
func ControlFiles(info Info) (map[string][]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(info.Control))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, controlLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > controlLimit {
		return nil, fmt.Errorf("oversized control metadata")
	}
	return contents(raw)
}

func check(filename, name, version, arch string, trust *rsa.PublicKey, inspect bool) (Info, error) {
	var info Info
	file, err := os.Open(filename)
	if err != nil {
		return info, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var sig []byte
	if trust != nil {
		sig, err = signature(file, reader)
		if err != nil {
			return info, err
		}
	}
	control, raw, err := member(file, reader)
	if err != nil {
		return info, err
	}
	if trust != nil {
		if err := Verify(control, sig, trust); err != nil {
			return info, fmt.Errorf("APK signature: %w", err)
		}
	}
	files, err := contents(raw)
	if err != nil {
		return info, err
	}
	if inspect {
		for key := range files {
			if strings.HasPrefix(key, ".SIGN.") {
				control, raw, err = member(file, reader)
				if err != nil {
					return info, err
				}
				files, err = contents(raw)
				if err != nil {
					return info, err
				}
				break
			}
		}
	}
	data, ok := files[".PKGINFO"]
	if !ok {
		return info, fmt.Errorf("missing .PKGINFO")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		if slices.Contains([]string{"pkgname", "pkgver", "arch", "datahash"}, key) && fields[key] != "" {
			return info, fmt.Errorf("duplicate APK identity field")
		}
		fields[key] = value
	}
	if inspect {
		name, version, arch = fields["pkgname"], fields["pkgver"], fields["arch"]
	}
	if name == "" || version == "" || arch == "" || fields["pkgname"] != name || fields["pkgver"] != version || fields["arch"] != arch {
		return info, fmt.Errorf("APK metadata differs from expected package: %s", filename)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return info, err
	}
	position -= int64(reader.Buffered())
	magic, err := reader.Peek(2)
	if err != nil || !bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		return info, fmt.Errorf("missing APK payload")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, reader)
	if err != nil {
		return info, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != fields["datahash"] {
		return info, fmt.Errorf("APK payload hash mismatch: %s", filename)
	}
	return Info{Name: name, Version: version, Arch: arch, Control: control, PayloadOffset: position, Size: position + size}, nil
}

// Payload visits tar members without extracting package paths to the host.
func Payload(filename string, info Info, visit func(*tar.Header, io.Reader) error) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(info.PayloadOffset, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := path.Clean(header.Name)
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe APK payload path: %s", header.Name)
		}
		header.Name = strings.TrimPrefix(clean, "./")
		if err := visit(header, reader); err != nil {
			return err
		}
	}
}

func gzipBytes(data []byte) ([]byte, error) {
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// SignatureMember leaves out the tar end marker: APK concatenates signature,
// control and payload tar streams inside separate gzip members.
func SignatureMember(data []byte, key *rsa.PrivateKey) ([]byte, error) {
	sig, err := Sign(data, key)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: ".SIGN.RSA256." + KeyName, Mode: 0644, Size: int64(len(sig)), Uname: "root", Gname: "root", Format: tar.FormatUSTAR}); err != nil {
		return nil, err
	}
	if _, err := writer.Write(sig); err != nil {
		return nil, err
	}
	if err := writer.Flush(); err != nil {
		return nil, err
	}
	return gzipBytes(buffer.Bytes())
}

// SignPackage adds a signature to a validated unsigned APK.
func SignPackage(source, destination, name, version, arch string, key *rsa.PrivateKey) error {
	info, err := Check(source, name, version, arch, nil)
	if err != nil {
		return err
	}
	signature, err := SignatureMember(info.Control, key)
	if err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := output.Write(signature); err != nil {
		output.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	_, err = Check(destination, name, version, arch, &key.PublicKey)
	return err
}

// Index verifies a signed index and returns immutable stanzas by APK filename.
func Index(filename, arch string, trust *rsa.PublicKey) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	sig, err := signature(file, reader)
	if err != nil {
		return nil, err
	}
	control, raw, err := member(file, reader)
	if err != nil {
		return nil, err
	}
	if err := Verify(control, sig, trust); err != nil {
		return nil, err
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return nil, fmt.Errorf("extra data after signed index")
	}
	files, err := contents(raw)
	if err != nil {
		return nil, err
	}
	data, ok := files["APKINDEX"]
	if !ok {
		return nil, fmt.Errorf("missing APKINDEX")
	}
	entries := map[string]string{}
	for _, stanza := range strings.Split(strings.TrimSpace(string(data)), "\n\n") {
		if stanza == "" {
			continue
		}
		fields := Fields(stanza)
		seen := map[string]bool{}
		for _, line := range strings.Split(stanza, "\n") {
			key, _, ok := strings.Cut(line, ":")
			if !ok || len(key) != 1 || seen[key] {
				return nil, fmt.Errorf("invalid or duplicate index field")
			}
			seen[key] = true
		}
		name := fields["P"] + "-" + fields["V"] + ".apk"
		if fields["A"] != arch || !indexedName.MatchString(fields["P"]) || !indexedVersion.MatchString(fields["V"]) {
			return nil, fmt.Errorf("unsafe indexed identity")
		}
		checksum, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(fields["C"], "Q1"))
		size, sizeErr := strconv.ParseInt(fields["S"], 10, 64)
		if !strings.HasPrefix(fields["C"], "Q1") || err != nil || len(checksum) != sha1.Size || sizeErr != nil || size <= 0 {
			return nil, fmt.Errorf("invalid indexed checksum or size")
		}
		if _, ok := entries[name]; ok {
			return nil, fmt.Errorf("duplicate indexed filename")
		}
		entries[name] = stanza
	}
	return entries, nil
}

// Fields reads an index stanza's colon-separated fields.
func Fields(stanza string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(stanza, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = value
		}
	}
	return fields
}

// Match checks the index's control checksum and file size against an APK.
func Match(info Info, stanza string) error {
	fields := Fields(stanza)
	sum := sha1.Sum(info.Control)
	if fields["P"] != info.Name || fields["V"] != info.Version || fields["A"] != info.Arch || fields["C"] != "Q1"+base64.StdEncoding.EncodeToString(sum[:]) || fields["S"] != strconv.FormatInt(info.Size, 10) {
		return fmt.Errorf("signed index differs from APK: %s", info.Name)
	}
	return nil
}

// SignedIndex creates a deterministic signed APKINDEX containing every version.
func SignedIndex(entries map[string]string, key *rsa.PrivateKey) ([]byte, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	var text strings.Builder
	for _, name := range names {
		text.WriteString(entries[name] + "\n\n")
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, file := range []struct{ name, data string }{{"APKINDEX", text.String()}, {"DESCRIPTION", ""}} {
		if err := writer.WriteHeader(&tar.Header{Name: file.name, Size: int64(len(file.data)), Mode: 0644, Format: tar.FormatUSTAR}); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(writer, file.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	control, err := gzipBytes(buffer.Bytes())
	if err != nil {
		return nil, err
	}
	signature, err := SignatureMember(control, key)
	if err != nil {
		return nil, err
	}
	return append(signature, control...), nil
}
