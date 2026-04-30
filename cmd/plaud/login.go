package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/auth"
)

// loginCmdOpts carries dependencies the login command needs. Tests inject a
// custom resolver to point HTTP at an httptest server; production wires
// api.BaseURL.
type loginCmdOpts struct {
	resolveBaseURL func(api.Region) (string, error)
}

type loginOption func(*loginCmdOpts)

// withBaseURLResolver overrides the region-to-URL resolver. Test seam.
func withBaseURLResolver(f func(api.Region) (string, error)) loginOption {
	return func(o *loginCmdOpts) { o.resolveBaseURL = f }
}

func newLoginCmd(opts ...loginOption) *cobra.Command {
	o := &loginCmdOpts{resolveBaseURL: api.BaseURL}
	for _, opt := range opts {
		opt(o)
	}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Plaud.ai with email + OTP code",
		Long: `Log in to your Plaud.ai account.

The CLI prompts for region (us, eu, jp), email, and the 6-digit code Plaud
emails to you. The bearer token is saved at the standard config location for
your OS so subsequent commands can reuse it.

This tool is unofficial and not affiliated with PLAUD LLC.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), o)
		},
	}
	return cmd
}

func runLogin(ctx context.Context, stdin io.Reader, stdout io.Writer, o *loginCmdOpts) error {
	scanner := bufio.NewScanner(stdin)

	region, err := promptRegion(stdout, scanner)
	if err != nil {
		return err
	}
	baseURL, err := o.resolveBaseURL(region)
	if err != nil {
		return fmt.Errorf("resolving region: %w", err)
	}

	email, err := promptLine(stdout, scanner, "Email: ")
	if err != nil {
		return err
	}
	if email == "" {
		return errors.New("email is required")
	}

	userArea := detectUserArea(os.Getenv("LANG"), os.Getenv("LC_ALL"))

	fmt.Fprintln(stdout, "Sending OTP code to your email...")
	exchangeToken, err := api.SendOTP(ctx, baseURL, email, userArea)
	if err != nil {
		return fmt.Errorf("requesting OTP: %w", err)
	}

	code, err := promptLine(stdout, scanner, "Enter the 6-digit code from your email: ")
	if err != nil {
		return err
	}
	if code == "" {
		return errors.New("OTP code is required")
	}

	bearer, err := api.VerifyOTP(ctx, baseURL, exchangeToken, code, userArea)
	if errors.Is(err, api.ErrPasswordNotSet) {
		return errors.New(
			"your Plaud account does not have a password set. " +
				"Open https://web.plaud.ai, set a password under Account, then run `plaud login` again",
		)
	}
	if err != nil {
		return fmt.Errorf("verifying OTP: %w", err)
	}

	if err := auth.Save(auth.Credentials{
		Token:      bearer,
		Region:     string(region),
		Email:      email,
		ObtainedAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	fmt.Fprintln(stdout, "Logged in successfully.")
	return nil
}

func promptRegion(out io.Writer, scanner *bufio.Scanner) (api.Region, error) {
	for {
		val, err := promptLine(out, scanner, "Region (us/eu/jp): ")
		if err != nil {
			return "", err
		}
		r := api.Region(strings.ToLower(strings.TrimSpace(val)))
		if _, err := api.BaseURL(r); err == nil {
			return r, nil
		}
		fmt.Fprintf(out, "Unknown region %q. Please enter one of: us, eu, jp.\n", val)
	}
}

func promptLine(out io.Writer, scanner *bufio.Scanner, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading input: %w", err)
		}
		return "", errors.New("input closed before value provided")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// detectUserArea derives the 2-letter ISO 3166-1 country code from a POSIX
// locale string of the form "ll_CC[.ENC][@MOD]". Returns "US" when no
// recognizable country part is present.
//
// LC_ALL takes precedence over LANG, matching POSIX locale resolution.
func detectUserArea(langEnv, lcAllEnv string) string {
	for _, v := range []string{lcAllEnv, langEnv} {
		if v == "" {
			continue
		}
		// Strip codeset (after .) and modifier (after @)
		if i := strings.IndexAny(v, ".@"); i >= 0 {
			v = v[:i]
		}
		// Expect "ll_CC"
		i := strings.Index(v, "_")
		if i < 0 || len(v) < i+3 {
			continue
		}
		return strings.ToUpper(v[i+1 : i+3])
	}
	return "US"
}
