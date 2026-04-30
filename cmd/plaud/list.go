package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/auth"
)

type listCmdOpts struct {
	resolveBaseURL func(api.Region) (string, error)
}

type listOption func(*listCmdOpts)

// withListBaseURLResolver overrides the region-to-URL resolver. Test seam.
func withListBaseURLResolver(f func(api.Region) (string, error)) listOption {
	return func(o *listCmdOpts) { o.resolveBaseURL = f }
}

func newListCmd(opts ...listOption) *cobra.Command {
	o := &listCmdOpts{resolveBaseURL: api.BaseURL}
	for _, opt := range opts {
		opt(o)
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every recording on your Plaud.ai account",
		Long: `List every recording on your Plaud.ai account, sorted newest first.

Output is a table of date, title, duration (HH:MM:SS), and recording id.

Requires a prior 'plaud login' (or 'plaud login --token').`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), o)
		},
	}
	return cmd
}

func runList(ctx context.Context, stdout, stderr io.Writer, o *listCmdOpts) error {
	creds, err := auth.Load()
	if errors.Is(err, auth.ErrNotLoggedIn) {
		fmt.Fprintln(stderr, "Not logged in. Run `plaud login` first.")
		return errors.New("not logged in")
	}
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}

	region := api.Region(creds.Region)
	baseURL, err := o.resolveBaseURL(region)
	if err != nil {
		return fmt.Errorf("resolving region %q: %w", creds.Region, err)
	}

	client, err := api.New(region, creds.Token, api.WithBaseURL(baseURL))
	if err != nil {
		return fmt.Errorf("constructing API client: %w", err)
	}

	recs, err := client.List(ctx)
	if errors.Is(err, api.ErrUnauthorized) {
		fmt.Fprintln(stderr, "Token expired or invalid. Run `plaud login` again.")
		return errors.New("unauthorized")
	}
	if err != nil {
		return fmt.Errorf("listing recordings: %w", err)
	}

	renderTable(stdout, recs)
	return nil
}

func renderTable(out io.Writer, recs []api.Recording) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATE\tTITLE\tDURATION\tID")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.StartTime.Format("2006-01-02 15:04"),
			r.Filename,
			formatDuration(r.Duration),
			r.ID,
		)
	}
	_ = tw.Flush()
}

// formatDuration renders a Duration as HH:MM:SS. Recordings longer than
// 99 hours overflow the format; not a concern in practice.
func formatDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
