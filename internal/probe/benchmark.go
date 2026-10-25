package probe

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/werewolf-linux/packages/internal/apk"
)

type BenchmarkOptions struct {
	BaselineLibc, BaselineLoader, CandidateLibc, CandidateLoader string
	Fixtures, Output                                             string
	CC                                                           []string
	Rounds                                                       int
}

func extractLibraries(filename, root string, info apk.Info) error {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	type link struct {
		name, target string
		hard         bool
	}
	var links []link
	seen := map[string]bool{}
	total := int64(0)
	err = apk.Payload(filename, info, func(header *tar.Header, reader io.Reader) error {
		if !strings.HasPrefix(header.Name, "usr/lib/") {
			return nil
		}
		if seen[header.Name] {
			return fmt.Errorf("duplicate runtime library: %s", header.Name)
		}
		seen[header.Name] = true
		target := filepath.FromSlash(header.Name)
		if header.Typeflag == tar.TypeDir {
			return directory.MkdirAll(target, 0755)
		}
		if err := directory.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size > 512<<20 || total > 1<<30 {
				return fmt.Errorf("oversized runtime libraries")
			}
			file, err := directory.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		case tar.TypeSymlink, tar.TypeLink:
			destination := header.Linkname
			if header.Typeflag == tar.TypeSymlink && !path.IsAbs(destination) {
				destination = path.Join(path.Dir(header.Name), destination)
			} else {
				destination = strings.TrimPrefix(destination, "/")
			}
			if !strings.HasPrefix(destination, "usr/lib/") || !filepath.IsLocal(filepath.FromSlash(destination)) {
				return fmt.Errorf("runtime library link escapes private root: %s", header.Name)
			}
			links = append(links, link{header.Name, destination, header.Typeflag == tar.TypeLink})
			return nil
		default:
			return fmt.Errorf("unsupported runtime library member: %s", header.Name)
		}
	})
	if err != nil {
		return err
	}
	// Create links only after all regular files, so archive paths cannot write
	// through a link. OpenRoot also confines resolution of link chains.
	for _, link := range links {
		if link.hard {
			if err := directory.Link(filepath.FromSlash(link.target), filepath.FromSlash(link.name)); err != nil {
				return err
			}
		} else {
			relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(link.name)), filepath.FromSlash(link.target))
			if err != nil {
				return err
			}
			if err := directory.Symlink(relative, filepath.FromSlash(link.name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Benchmark compares explicitly chosen native APKs, retaining every sample.
func Benchmark(ctx context.Context, options BenchmarkOptions) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("benchmark requires native Linux")
	}
	if options.Rounds < 5 {
		return fmt.Errorf("at least five interleaved rounds required")
	}
	cc := options.CC
	if len(cc) == 0 {
		cc = []string{"gcc"}
	}
	compiler, err := checked(ctx, append(slices.Clone(cc), "--version")...)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "glibc-benchmark-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	evidence := map[string]any{}
	binaries := map[string]string{}
	source, architecture := "", map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if architecture == "" {
		return fmt.Errorf("unsupported benchmark architecture")
	}
	for _, selection := range []struct {
		label string
		files []string
	}{{"baseline", []string{options.BaselineLibc, options.BaselineLoader}}, {"candidate", []string{options.CandidateLibc, options.CandidateLoader}}} {
		root := filepath.Join(work, selection.label)
		if err := os.Mkdir(root, 0755); err != nil {
			return err
		}
		var packages []map[string]string
		for _, filename := range selection.files {
			info, err := apk.Inspect(filename)
			if err != nil {
				return err
			}
			record, err := packageIdentity(filename, info)
			if err != nil {
				return err
			}
			if record.Arch != architecture || source != "" && source != record.Source {
				return fmt.Errorf("source commits or native architectures differ")
			}
			source = record.Source
			if err := extractLibraries(filename, root, info); err != nil {
				return err
			}
			packages = append(packages, map[string]string{"name": filepath.Base(filename), "sha256": record.SHA256})
		}
		evidence[selection.label] = packages
		lib := filepath.Join(root, "usr/lib")
		loaders, err := filepath.Glob(filepath.Join(lib, "ld-linux-*.so.*"))
		if err != nil || len(loaders) != 1 {
			return fmt.Errorf("expected exactly one runtime loader")
		}
		binary := filepath.Join(root, "benchmark")
		args := append(slices.Clone(cc), "-O2", "-fno-builtin", "-Wall", "-Wextra", "-Werror", "-pthread", options.Fixtures, filepath.Join(lib, "libc.so.6"), "-Wl,--dynamic-linker="+loaders[0], "-Wl,--disable-new-dtags", "-Wl,-rpath,"+lib, "-o", binary)
		if _, err := checked(ctx, args...); err != nil {
			return err
		}
		headers, err := checked(ctx, "readelf", "-lWd", binary)
		if err != nil {
			return err
		}
		evidence[selection.label+"-elf"] = headers.stdout
		binaries[selection.label] = binary
	}
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(key, "LD_") && !strings.HasPrefix(key, "MALLOC_") && !strings.HasPrefix(key, "LC_") && key != "GLIBC_TUNABLES" && key != "LANG" && key != "LANGUAGE" {
			values[key] = value
		}
	}
	env := environment(values)
	generator := rand.New(rand.NewPCG(0, 0))
	samples := map[string]map[string][]float64{}
	medians := map[string]map[string]float64{}
	for _, mode := range []string{"startup", "allocate", "threads", "memory", "identity", "hosts-file"} {
		data := map[string][]float64{"baseline": {}, "candidate": {}}
		for _, label := range []string{"baseline", "candidate"} {
			value, err := execute(ctx, 0, "", env, binaries[label], mode)
			if err != nil {
				return err
			}
			if value.code != 0 {
				return fmt.Errorf("warmup %s %s: %s", label, mode, value.stderr)
			}
		}
		for round := 0; round < options.Rounds; round++ {
			order := []string{"baseline", "candidate"}
			generator.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			for _, label := range order {
				count := 1
				if mode == "startup" {
					count = 200
				}
				start := time.Now()
				var output result
				for range count {
					output, err = execute(ctx, 0, "", env, binaries[label], mode)
					if err != nil {
						return err
					}
					if output.code != 0 {
						return fmt.Errorf("benchmark %s %s: %s", label, mode, output.stderr)
					}
				}
				elapsed := time.Since(start).Seconds() / float64(count)
				if mode != "startup" {
					elapsed, err = strconv.ParseFloat(strings.TrimSpace(output.stdout), 64)
					if err != nil {
						return err
					}
				}
				if elapsed <= 0 || math.IsNaN(elapsed) || math.IsInf(elapsed, 0) {
					return fmt.Errorf("invalid benchmark measurement: %v", elapsed)
				}
				data[label] = append(data[label], elapsed)
			}
		}
		samples[mode] = data
		median := map[string]float64{}
		for label, values := range data {
			sorted := slices.Sorted(slices.Values(values))
			middle := len(sorted) / 2
			value := sorted[middle]
			if len(sorted)%2 == 0 {
				value = (sorted[middle-1] + value) / 2
			}
			median[label] = value
		}
		median["regression_percent"] = 100 * (median["candidate"]/median["baseline"] - 1)
		medians[mode] = median
		slog.Info("measured libc microbenchmark", "mode", mode, "regression_percent", median["regression_percent"])
	}
	host, err := checked(ctx, "uname", "-a")
	if err != nil {
		return err
	}
	return writeReport(options.Output, map[string]any{"host": host.stdout, "rounds": options.Rounds, "packages": evidence, "samples": samples, "medians": medians, "compiler": compiler.stdout, "source_commit": source})
}
