package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"mcpscope/cmd"
)

var version = "dev"

func main() {
	dashboardFS, err := fs.Sub(embeddedDashboard, "dashboard/dist")
	if err != nil {
		slog.Error("failed to load embedded dashboard", "error", err)
		os.Exit(1)
	}

	cmd.SetDashboardFS(dashboardFS)
	cmd.SetVersion(version)
	cmd.SetBuildInfo(fmt.Sprintf("%s, %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))

	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Fprintln(os.Stdout, cmd.VersionString())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		if exitErr, ok := cmd.AsExitCoder(err); ok {
			os.Exit(exitErr.ExitCode())
		}
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
