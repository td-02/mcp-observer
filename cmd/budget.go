package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mcpscope/internal/budget"
	"mcpscope/internal/store"
)

func init() {
	rootCmd.AddCommand(newBudgetCmd())
}

func newBudgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Inspect and reset per-team budgets",
	}
	cmd.AddCommand(newBudgetResetCmd())
	return cmd
}

func newBudgetResetCmd() *cobra.Command {
	var teamID string
	var window string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset the current budget window for a team",
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID = strings.TrimSpace(teamID)
			if teamID == "" {
				return fmt.Errorf("--team is required")
			}

			windowType, err := parseBudgetWindow(window)
			if err != nil {
				return err
			}

			traceStore, err := store.OpenSQLite(cmd.Context(), dbPath)
			if err != nil {
				return err
			}
			defer traceStore.Close()

			now := time.Now().UTC()
			if err := traceStore.ResetBudgetWindow(cmd.Context(), teamID, string(windowType), budget.WindowStart(now, windowType)); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "reset %s budget window for team %s\n", windowType, teamID)
			return err
		},
	}

	cmd.Flags().StringVar(&teamID, "team", "", "Team ID to reset")
	cmd.Flags().StringVar(&window, "window", "hour", "Budget window to reset: hour or day")
	cmd.Flags().StringVar(&dbPath, "db", "mcpscope.db", "SQLite database path for persisted traces")
	return cmd
}

func parseBudgetWindow(raw string) (budget.WindowType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(budget.WindowHour):
		return budget.WindowHour, nil
	case string(budget.WindowDay):
		return budget.WindowDay, nil
	default:
		return "", fmt.Errorf("--window must be hour or day")
	}
}
