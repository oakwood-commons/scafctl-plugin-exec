// Package main is the entry point for the scafctl-plugin-exec plugin.
package main

import (
	execprovider "github.com/oakwood-commons/scafctl-plugin-exec/internal/exec"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

func main() {
	sdkplugin.Serve(execprovider.NewPlugin())
}
