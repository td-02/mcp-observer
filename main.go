package main

import (
	"io/fs"
	"log"
	"os"

	"mcpscope/cmd"
)

var version = "dev"

func main() {
	dashboardFS, err := fs.Sub(embeddedDashboard, "dashboard/dist")
	if err != nil {
		log.Fatal(err)
	}

	cmd.SetDashboardFS(dashboardFS)
	cmd.SetVersion(version)

	if err := cmd.Execute(); err != nil {
		if exitErr, ok := cmd.AsExitCoder(err); ok {
			os.Exit(exitErr.ExitCode())
		}
		log.Fatal(err)
	}
}
