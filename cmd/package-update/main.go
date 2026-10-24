package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/werewolf-linux/packages/internal/update"
)

func main() {
	discoverRelease := flag.Bool("discover-release", false, "read an upstream policy from stdin and resolve its stable release commit")
	root := flag.String("root", ".", "repository root")
	write := flag.Bool("write", false, "prepare and write complete updates (default: discovery only)")
	validate := flag.Bool("validate", false, "validate policy coverage offline")
	selected := flag.String("package", "", "one context; empty checks every context")
	report := flag.String("report", "", "also save the JSON report to this file")
	flag.Parse()
	if *discoverRelease {
		var policy update.Policy
		decoder := json.NewDecoder(io.LimitReader(os.Stdin, 65536))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		release, err := update.NewClient(os.Getenv("GH_TOKEN")).ResolveRelease(ctx, policy)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(release); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	policies, err := update.LoadPolicies(absolute)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *validate {
		fmt.Printf("%d package policies validated\n", len(policies))
		return
	}
	names := update.Names(policies)
	if *selected != "" {
		if _, ok := policies[*selected]; !ok {
			fmt.Fprintln(os.Stderr, "unknown package:", *selected)
			os.Exit(2)
		}
		names = []string{*selected}
	}
	client := update.NewClient(os.Getenv("GH_TOKEN"))
	results := make([]update.Result, 0, len(names))
	failed := false
	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		r := client.Update(ctx, absolute, name, policies[name], *write, update.RunCommand)
		cancel()
		fmt.Fprintf(os.Stderr, "%s: %s %s -> %s %s\n", name, r.Status, r.Current, r.Latest, r.Error)
		results = append(results, r)
		failed = failed || r.Status == "failed"
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data = append(data, '\n')
	if *report != "" {
		if err := os.WriteFile(*report, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Stdout.Write(data)
	if failed {
		os.Exit(1)
	}
}
