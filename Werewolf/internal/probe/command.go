package probe

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// Command exposes the same probes to native development and build guests.
func Command(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("probe requires curl, glibc, glibc-locales, glibc-compare, glibc-benchmark or keycloak")
	}
	if args[0] == "json-check" {
		if len(args) < 2 {
			return fmt.Errorf("json-check requires file paths")
		}
		for _, path := range args[1:] {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			data, readErr := io.ReadAll(io.LimitReader(file, (16<<20)+1))
			closeErr := file.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if len(data) > 16<<20 || !json.Valid(data) {
				return fmt.Errorf("invalid or oversized JSON: %s", path)
			}
		}
		return nil
	}
	if args[0] == "keycloak" {
		if len(args) != 1 {
			return fmt.Errorf("keycloak takes no arguments")
		}
		return Keycloak(ctx)
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "curl":
		var options CurlOptions
		flags.StringVar(&options.Binary, "binary", "", "installed curl executable")
		flags.BoolVar(&options.Musl, "musl", false, "require the private musl linkage")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || options.Binary == "" {
			return fmt.Errorf("curl requires --binary and no positional arguments")
		}
		return Curl(ctx, options)
	case "glibc":
		var options GlibcOptions
		flags.StringVar(&options.Build, "build", "", "built glibc directory")
		flags.StringVar(&options.Fixtures, "fixtures", "devel/glibc/tests", "C fixture directory")
		flags.StringVar(&options.BaselineLoader, "baseline-loader", "auto", "unmodified loader for positive controls")
		flags.StringVar(&options.Report, "report", "", "policy evidence output")
		flags.BoolVar(&options.Secure, "secure", false, "exercise native setuid behavior")
		flags.BoolVar(&options.TraceSpawn, "trace-spawn", false, "trace child environments")
		flags.Func("runner", "emulator executable or argument; repeat for each argument", func(value string) error {
			options.Runner = append(options.Runner, value)
			return nil
		})
		cc := flags.String("cc", "gcc", "C compiler executable")
		cxx := flags.String("cxx", "g++", "C++ compiler executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || options.Build == "" {
			return fmt.Errorf("glibc requires --build and no positional arguments")
		}
		options.CC, options.CXX = []string{*cc}, []string{*cxx}
		return Glibc(ctx, options)
	case "glibc-locales":
		var options LocaleOptions
		flags.StringVar(&options.Build, "build", "", "built glibc directory")
		flags.StringVar(&options.Source, "source", "", "glibc source directory")
		flags.StringVar(&options.Makefile, "makefile", "devel/glibc/tests/locales.mk", "fixture makefile")
		flags.StringVar(&options.Destination, "dest", "/usr/lib/locale", "compiled locale path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || options.Build == "" {
			return fmt.Errorf("glibc-locales requires --build and no positional arguments")
		}
		return Locales(ctx, options)
	case "glibc-compare":
		output := flags.String("output", "", "JSON report")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 2 || *output == "" {
			return fmt.Errorf("glibc-compare --output FILE BASELINE.apk CANDIDATE.apk")
		}
		return CompareResults(flags.Arg(0), flags.Arg(1), *output)
	case "glibc-benchmark":
		var options BenchmarkOptions
		flags.StringVar(&options.BaselineLibc, "baseline-libc", "", "baseline libc APK")
		flags.StringVar(&options.BaselineLoader, "baseline-loader", "", "baseline loader APK")
		flags.StringVar(&options.CandidateLibc, "candidate-libc", "", "candidate libc APK")
		flags.StringVar(&options.CandidateLoader, "candidate-loader", "", "candidate loader APK")
		flags.StringVar(&options.Fixtures, "fixture", "devel/glibc/tests/benchmark.c", "benchmark C source")
		flags.StringVar(&options.Output, "output", "", "JSON report")
		flags.IntVar(&options.Rounds, "rounds", 9, "interleaved sample rounds")
		cc := flags.String("cc", "gcc", "C compiler executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || options.BaselineLibc == "" || options.BaselineLoader == "" || options.CandidateLibc == "" || options.CandidateLoader == "" || options.Output == "" {
			return fmt.Errorf("glibc-benchmark requires four APK paths and --output")
		}
		options.CC = []string{*cc}
		return Benchmark(ctx, options)
	default:
		return fmt.Errorf("unknown probe %q", args[0])
	}
}
