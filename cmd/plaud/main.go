// Command plaud is an unofficial CLI for archiving Plaud.ai recordings,
// transcripts, and summaries into local storage.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newLogoutCmd())
	cmd.AddCommand(newDownloadCmd())
	cmd.AddCommand(newSyncCmd())

	return cmd
}

func main() {
	// Cancel the cobra command's context on SIGINT/SIGTERM so long-running
	// commands (sync, sync --watch) drain cleanly. Spec 0003 F-04/F-05.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
