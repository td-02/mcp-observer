package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mcpscope/internal/appconfig"
)

var rootCmd = &cobra.Command{
	Use:           "mcpscope",
	Short:         "MCP utility CLI",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initLogger(logLevel)
		cfg, err := appconfig.Load(configPath)
		if err != nil {
			return err
		}
		loadedConfig = cfg
		return nil
	},
}

var dashboardFS fs.FS
var buildVersion = "dev"
var configPath string
var logLevel string
var buildInfo = ""
var loadedConfig appconfig.Config

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a mcpscope YAML config file")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
}

type exitCodeError struct {
	code int
	err  error
}

func (e exitCodeError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e exitCodeError) Unwrap() error {
	return e.err
}

func (e exitCodeError) ExitCode() int {
	return e.code
}

func SetDashboardFS(static fs.FS) {
	dashboardFS = static
}

func SetVersion(version string) {
	if version == "" {
		buildVersion = "dev"
		return
	}
	buildVersion = version
}

func SetBuildInfo(info string) {
	buildInfo = strings.TrimSpace(info)
}

func VersionString() string {
	return versionString()
}

func AsExitCoder(err error) (interface{ ExitCode() int }, bool) {
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) {
		return exitErr, true
	}
	return nil, false
}

func Execute() error {
	return ExecuteContext(context.Background())
}

func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func initLogger(levelRaw string) {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelRaw)) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
}

func versionString() string {
	if buildInfo != "" {
		return fmt.Sprintf("mcpscope %s (%s)", buildVersion, buildInfo)
	}
	return fmt.Sprintf("mcpscope %s", buildVersion)
}
