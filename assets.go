package main

import "embed"

//go:embed dashboard/dist dashboard/dist/* dashboard/dist/assets/*
var embeddedDashboard embed.FS
