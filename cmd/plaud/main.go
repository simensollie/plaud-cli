// Command plaud is an unofficial CLI for archiving Plaud.ai recordings,
// transcripts, and summaries into local storage.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plaud",
		Short: "Archive your Plaud.ai recordings, transcripts, and summaries locally",
		Long: `plaud is a CLI for archiving recordings, transcripts, and summaries
from your Plaud.ai account into local storage.

This is an unofficial community tool. It is not affiliated with,
endorsed by, or sponsored by PLAUD LLC.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newLoginCmd())

	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
