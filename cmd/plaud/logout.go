package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/simensollie/plaud-cli/internal/auth"
)

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Forget the locally stored Plaud bearer token",
		Long: `Delete the credentials file plaud login created.

This only removes the local token; it does not invalidate the session on
Plaud's side. To invalidate the token everywhere, log out from web.plaud.ai
or rotate it by logging in again.

Idempotent: running logout when no credentials exist is a quiet success.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.OutOrStdout())
		},
	}
	return cmd
}

func runLogout(out io.Writer) error {
	if err := auth.Delete(); err != nil {
		return fmt.Errorf("removing credentials: %w", err)
	}
	fmt.Fprintln(out, "Logged out.")
	return nil
}
