package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/werewolf-linux/packages/Werewolf/internal/apk"
	"github.com/werewolf-linux/packages/Werewolf/internal/update"
)

// ToolPin is the single maintained Melange binary pin.
type ToolPin struct {
	Version string
	Hashes  map[string]string
}

func melangePin(root string) (ToolPin, error) {
	data, err := os.ReadFile(sourcePath(root, "Werewolf/tools.lock.json"))
	if err != nil {
		return ToolPin{}, err
	}
	var tools struct {
		Melange map[string]string `json:"melange"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		return ToolPin{}, err
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(tools.Melange["version"]) {
		return ToolPin{}, fmt.Errorf("invalid Melange pin")
	}
	return ToolPin{Version: tools.Melange["version"], Hashes: tools.Melange}, nil
}

// InstallMelange downloads only the native, hash-pinned binary.
func InstallMelange(ctx context.Context, root, destination string) error {
	pin, err := melangePin(root)
	if err != nil {
		return err
	}
	arch := runtime.GOARCH
	system := runtime.GOOS
	expected := pin.Hashes[system+"_"+arch]
	if len(expected) != 64 {
		return fmt.Errorf("no Melange pin for %s/%s", system, arch)
	}
	filename := "melange_" + pin.Version + "_" + system + "_" + arch + ".tar.gz"
	url := "https://github.com/chainguard-dev/melange/releases/download/v" + pin.Version + "/" + filename
	directory, err := os.MkdirTemp("", "packages-melange-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	archive := filepath.Join(directory, "archive.tar.gz")
	client := update.NewClient("")
	for attempt := 0; attempt < 3; attempt++ {
		file, err := os.Create(archive)
		if err != nil {
			return err
		}
		sum, downloadErr := client.Download(ctx, url, file)
		closeErr := file.Close()
		if downloadErr == nil {
			if closeErr != nil {
				return closeErr
			}
			if sum != expected {
				return fmt.Errorf("Melange archive differs from pinned SHA-256")
			}
			break
		}
		if attempt == 2 {
			return downloadErr
		}
		slog.Warn("Melange download failed; retrying", "attempt", attempt+1, "error", downloadErr)
		timer := time.NewTimer(time.Duration(5+10*attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	member := strings.TrimSuffix(filename, ".tar.gz") + "/melange"
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return fmt.Errorf("archive lacks Melange binary")
		}
		if err != nil {
			return err
		}
		if header.Name != member {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 512<<20 {
			return fmt.Errorf("invalid Melange archive member")
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		return writeFile(destination, data, 0755)
	}
}

// SigningKey installs a release credential with restricted permissions, or
// creates an ephemeral key for local package tests. Secrets never enter argv.
func SigningKey(root string, production bool) (string, error) {
	target := filepath.Join(root, "Werewolf/.work/keys/werewolf-packages.rsa")
	value := os.Getenv("PACKAGES_APK_SIGNING_KEY")
	if err := os.Unsetenv("PACKAGES_APK_SIGNING_KEY"); err != nil {
		return "", err
	}
	var key *rsa.PrivateKey
	if value != "" {
		var err error
		key, err = apk.PrivateKey([]byte(value))
		if err != nil {
			return "", err
		}
	} else {
		data, err := os.ReadFile(target)
		if err == nil {
			key, err = apk.PrivateKey(data)
			if err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		} else {
			if production {
				return "", fmt.Errorf("release signing credential is required")
			}
			key, err = rsa.GenerateKey(rand.Reader, 3072)
			if err != nil {
				return "", err
			}
			slog.Info("created ephemeral signing key", "path", target)
		}
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})
	if production || value != "" {
		trust, err := os.ReadFile(sourcePath(root, "Werewolf/keys/packages.pub"))
		if err != nil {
			return "", err
		}
		pinned, err := apk.PublicKey(trust)
		if err != nil {
			return "", err
		}
		if key.E != pinned.E || key.N.Cmp(pinned.N) != 0 {
			return "", fmt.Errorf("signing key differs from Werewolf/keys/packages.pub")
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := writeFile(target, private, 0600); err != nil {
		return "", err
	}
	if err := writeFile(target+".pub", publicPEM, 0644); err != nil {
		return "", err
	}
	return target, nil
}

// RunnerProfile derives a dedicated attachment from Ubuntu's bubblewrap profile.
func RunnerProfile(template, executable string) (string, error) {
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`).MatchString(executable) {
		return "", fmt.Errorf("runner path is not a literal AppArmor attachment")
	}
	sum := sha256.Sum256([]byte(executable))
	name := "werewolf_packages_bwrap_" + hex.EncodeToString(sum[:])[:16]
	child := name + "_child"
	marker := "profile unpriv_bwrap flags="
	if strings.Count(template, marker) != 1 {
		return "", fmt.Errorf("review changed Ubuntu bubblewrap child profile")
	}
	childRules := strings.SplitN(template, marker, 2)[1]
	var rules strings.Builder
	for _, line := range strings.Split(childRules, "\n") {
		rules.WriteString(strings.SplitN(line, "#", 2)[0] + "\n")
	}
	if regexp.MustCompile(`\bcapability\b`).MatchString(strings.ReplaceAll(rules.String(), "audit deny capability,", "")) {
		return "", fmt.Errorf("unexpected child capability rules")
	}
	replacements := [][2]string{
		{"profile bwrap /usr/bin/bwrap", "profile " + name + " \"" + executable + "\""},
		{"&bwrap//&unpriv_bwrap", "&" + name + "//&" + child},
		{marker, "profile " + child + " flags="}, {"&unpriv_bwrap", "&" + child},
		// The nested smoke runner mounts a read-only root in its own user
		// namespace, then drops every capability before running the app.
		{"audit deny capability,", `allow capability sys_chroot,
    allow capability setuid,
    allow capability setgid,
    allow capability chown,
    allow capability dac_override,
    allow capability sys_admin,
    allow capability setfcap,
    allow capability net_admin,
    allow capability net_bind_service,
    allow capability setpcap,`},
		{"include if exists <local/bwrap-userns-restrict>", ""}, {"include if exists <local/unpriv_bwrap>", ""},
	}
	for _, pair := range replacements {
		if strings.Count(template, pair[0]) != 1 {
			return "", fmt.Errorf("review changed Ubuntu profile: %s", pair[0])
		}
		template = strings.Replace(template, pair[0], pair[1], 1)
	}
	if strings.Contains(template, "unconfined") {
		return "", fmt.Errorf("refusing unconfined runner profile")
	}
	return template, nil
}

// SetupRunner loads the private builder's profile and verifies its capabilities.
func SetupRunner(ctx context.Context, root string) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("runner setup requires Linux root")
	}
	executable := filepath.Join(root, "Werewolf/.work/tools/bwrap")
	template, err := os.ReadFile("/etc/apparmor.d/bwrap-userns-restrict")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	binary := executable
	if err == nil {
		binary += "-private"
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(binary), ".bwrap-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := copyFile("/usr/bin/bwrap", temporary.Name()); err != nil {
		return err
	}
	// A build may still be executing the old inode while its profile is reloaded.
	if err := os.Rename(temporary.Name(), binary); err != nil {
		return err
	}
	if template != nil {
		policy, err := RunnerProfile(string(template), binary)
		if err != nil {
			return err
		}
		if err := writeFile(binary+".apparmor", []byte(policy), 0644); err != nil {
			return err
		}
		if err := Run(ctx, root, "/usr/sbin/apparmor_parser", "--replace", binary+".apparmor"); err != nil {
			return err
		}
		for _, pair := range [][2]string{{executable, binary}, {executable + ".apparmor", binary + ".apparmor"}} {
			link := pair[0] + ".new"
			if err := os.Symlink(filepath.Base(pair[1]), link); err != nil {
				return err
			}
			if err := os.Rename(link, pair[0]); err != nil {
				return err
			}
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := Run(ctx, root, executable, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--unshare-pid", "--cap-add", "CAP_SYS_CHROOT", "--cap-add", "CAP_CHOWN", "--cap-add", "CAP_DAC_OVERRIDE", "--", self, "runner-probe"); err != nil {
		return err
	}
	return Run(ctx, root, executable, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--unshare-user", "--uid", "0", "--gid", "0", "--cap-add", "CAP_SETFCAP", "--", "unshare", "--user", "--map-root-user", "true")
}

// RunnerProbe exercises the operations Melange's guest needs. It is called only
// by an explicit CLI subcommand inside a confined bubblewrap process.
func RunnerProbe(ctx context.Context) error {
	if err := syscall.Chroot("/"); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "packages-permissions-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "permissions")
	if err := os.WriteFile(path, []byte("probe"), 0000); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !bytes.Equal(data, []byte("probe")) {
		return fmt.Errorf("mode-000 file access failed")
	}
	if err := os.Chown(path, 65532, 65532); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 65532 || stat.Gid != 65532 {
		return fmt.Errorf("guest ownership change failed")
	}
	// Exercise the nested mount and privilege drop used by native smoke tests.
	if err := Run(ctx, "/", "/usr/bin/bwrap", "--unshare-user", "--unshare-pid", "--unshare-net",
		"--ro-bind", "/", "/", "--dev", "/dev", "--ro-bind", "/proc", "/proc",
		"--uid", "1000", "--gid", "1000", "--cap-drop", "ALL", "--", "/bin/sh", "-ec",
		`test "$(id -u)" = 1000; grep -q '^CapEff:[[:space:]]*0000000000000000$' /proc/self/status`); err != nil {
		return err
	}
	slog.Info("runner probe passed", "checks", []string{"chroot", "shadow-file access", "ownership", "unprivileged nested runtime"})
	return nil
}
