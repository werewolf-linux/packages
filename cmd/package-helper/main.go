// Command package-helper applies reviewed package data inside a build guest.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/werewolf-linux/packages/internal/catalog"
	"github.com/werewolf-linux/packages/internal/probe"
	"github.com/werewolf-linux/packages/internal/transform"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		if err := probe.Command(ctx, os.Args[1:]); err != nil {
			slog.Error("package probe failed", "error", err)
			os.Exit(1)
		}
		return
	}
	plan := flag.String("plan", "package.yaml", "package definition")
	root := flag.String("root", ".", "root of the files to change")
	action := flag.String("action", "", "named build action")
	flag.Parse()
	definition, err := catalog.Read(*plan)
	var spec transform.Plan
	if err == nil {
		spec.Actions = definition.Actions
	}
	if err == nil {
		selected, ok := spec.Actions[*action]
		if !ok {
			err = fmt.Errorf("unknown action %q", *action)
		} else if selected.Glibc != nil {
			err = probe.Glibc(ctx, *selected.Glibc)
		} else if selected.Locales != nil {
			err = probe.Locales(ctx, *selected.Locales)
		} else if selected.Curl != nil {
			err = probe.Curl(ctx, *selected.Curl)
		} else {
			err = transform.Apply(spec, *action, *root)
		}
	}
	if err != nil {
		slog.Error("package transformation failed", "action", *action, "error", err)
		os.Exit(1)
	}
}
