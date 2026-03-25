package cmd

import (
	"errors"
	"io/fs"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "mcpscope",
	Short:         "MCP utility CLI",
	SilenceUsage:  true,
	SilenceErrors: true,
}

var dashboardFS fs.FS

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

func AsExitCoder(err error) (interface{ ExitCode() int }, bool) {
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) {
		return exitErr, true
	}
	return nil, false
}

func Execute() error {
	return rootCmd.Execute()
}
