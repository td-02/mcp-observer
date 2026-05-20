package cmd

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcpscope/internal/auditexport"
	"mcpscope/internal/store"
)

func init() {
	rootCmd.AddCommand(newExportCmd())
}

func newExportCmd() *cobra.Command {
	var dbPath string
	var outputPath string
	var format string
	var workspace string
	var environment string
	var method string
	var status string
	var from string
	var to string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export traces as CSV or NDJSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath = effectiveString(cmd, "db", dbPath, loadedConfig.Proxy.DB)
			format = strings.ToLower(strings.TrimSpace(format))
			if format == "" {
				format = auditexport.FormatJSON
			}

			traceStore, err := store.OpenSQLite(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer traceStore.Close()

			rows, err := openExportRows(cmd.Context(), traceStore, workspace, environment, method, status, from, to)
			if err != nil {
				return err
			}
			defer rows.Close()

			writer := cmd.OutOrStdout()
			if outputPath != "" {
				file, err := os.Create(outputPath)
				if err != nil {
					return err
				}
				defer file.Close()
				writer = file
			}

			if err := auditexport.StreamRows(rows, format, writer); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	cmd.Flags().StringVar(&outputPath, "output", "", "Path to write exported traces")
	cmd.Flags().StringVar(&format, "format", "json", "Export format: csv or json")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Optional workspace filter")
	cmd.Flags().StringVar(&environment, "environment", "", "Optional environment filter")
	cmd.Flags().StringVar(&method, "method", "", "Optional MCP method filter")
	cmd.Flags().StringVar(&status, "status", "all", "Filter status: ok, error, or all")
	cmd.Flags().StringVar(&from, "from", "", "RFC3339 or relative start time such as 7d")
	cmd.Flags().StringVar(&to, "to", "", "RFC3339 or relative end time such as now")

	return cmd
}

func openExportRows(
	ctx context.Context,
	traceStore *store.SQLiteStore,
	workspace, environment, method, status, from, to string,
) (*sql.Rows, error) {
	filter, err := auditexport.BuildQueryFilter(auditexport.FilterInput{
		Workspace:   workspace,
		Environment: environment,
		Method:      method,
		Status:      status,
		From:        from,
		To:          to,
	}, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	return traceStore.QueryRows(ctx, filter)
}
