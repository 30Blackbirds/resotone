// Command resotone is the 30blackbirds headless CMS (Resotone) server.
//
// Subcommands:
//
//	resotone              run the server (default)
//	resotone healthcheck  probe /healthz on PORT and exit 0/1 (distroless
//	                      HEALTHCHECK — no curl/wget in the image)
//	resotone admin grant  make a user an administrator of a tenant (first admin)
//	resotone version      print the build version
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/30blackbirds/resotone/internal/app"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "migrate":
			os.Exit(runMigrate())
		case "admin":
			os.Exit(runAdmin(os.Args[2:]))
		case "seed":
			os.Exit(runSeed())
		case "sanitize-backfill":
			os.Exit(runSanitizeBackfill(os.Args[2:]))
		case "slug-audit":
			os.Exit(runSlugAudit())
		case "help", "-h", "--help":
			usage(os.Stdout)
			return
		case "version", "--version":
			_, _ = fmt.Fprintln(os.Stdout, app.Version())
			return
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Error("resotone exited with error", "error", err)
		os.Exit(1)
	}
}

// healthcheck probes the local /healthz endpoint and exits accordingly.
// Distroless ships no shell/curl, so the container HEALTHCHECK invokes the
// binary itself in this mode.
func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	if _, err := strconv.Atoi(port); err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: invalid PORT=%q\n", port)
		return 1
	}
	host := os.Getenv("HEALTHCHECK_HOST")
	if host == "" {
		host = "localhost"
	}

	timeout := 3 * time.Second
	if v := os.Getenv("HEALTHCHECK_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}

	url := fmt.Sprintf("http://%s:%s/healthz", host, port)
	c := &http.Client{Timeout: timeout}
	// #nosec G704 -- localhost /healthz probe; host/port stammer fra betroet
	// container-env (HEALTHCHECK_HOST/PORT), ikke bruger-input. Standard
	// distroless-healthcheck-mønster.
	resp, err := c.Get(url) //nolint:noctx // kortlivet probe med client-timeout
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// runMigrate runs pending DB migrations and exits. The deploy runs this as a
// one-shot as the migrator-role before starting the server (CTO Option A).
func runMigrate() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Migrate(ctx); err != nil {
		slog.Error("migrate failed", "error", err)
		return 1
	}
	return 0
}

// runSeed inserts the explainers demo dataset (RESONANS_SEED_TENANT) and exits.
func runSeed() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Seed(ctx); err != nil {
		slog.Error("seed failed", "error", err)
		return 1
	}
	return 0
}

// runAdmin handles `resotone admin grant`: make one user an administrator of a
// tenant. This is how the first admin comes to exist; it needs the database, so
// whoever can run it already controls the install.
func runAdmin(args []string) int {
	if len(args) == 0 || args[0] != "grant" {
		fmt.Fprintln(os.Stderr, "admin: the only subcommand is `grant` (see `resotone help`)")
		return 2
	}
	fs := flag.NewFlagSet("admin grant", flag.ContinueOnError)
	issuer := fs.String("issuer", "", "the identity provider's issuer URL (default RESONANS_JWT_ISSUER)")
	sub := fs.String("sub", "", "the `sub` claim in the person's token (required)")
	tenant := fs.String("tenant", "", "tenant uuid (default RESONANS_TENANT_ID in single-tenant mode)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "admin grant: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.AdminGrant(ctx, *issuer, *sub, *tenant); err != nil {
		slog.Error("admin grant failed", "error", err)
		return 1
	}
	return 0
}

// runSanitizeBackfill cleans stored rich text, and labels rich_text that the
// interim editor stored as HTML without a format so it gets sanitized at all.
// Rows it cannot classify are reported, never guessed at. Dry-run unless
// --apply.
func runSanitizeBackfill(args []string) int {
	apply := false
	for _, a := range args {
		switch a {
		case "--apply":
			apply = true
		default:
			fmt.Fprintf(os.Stderr, "sanitize-backfill: unknown argument %q (only --apply)\n", a)
			return 2
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.SanitizeBackfill(ctx, apply); err != nil {
		slog.Error("sanitize-backfill failed", "error", err)
		return 1
	}
	return 0
}

// runSlugAudit lists stored slugs that create now rejects. Read-only.
func runSlugAudit() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.SlugAudit(ctx); err != nil {
		slog.Error("slug-audit failed", "error", err)
		return 1
	}
	return 0
}

func usage(w *os.File) {
	_, _ = fmt.Fprintln(w, `resotone — 30blackbirds headless CMS server

Usage:
  resotone              run the server
  resotone migrate      run pending DB migrations and exit (as migrator role)
  resotone admin grant [--issuer URL] --sub SUB [--tenant UUID]
                        make a user an administrator of a tenant: the first admin.
                        Idempotent; creates the user record if they have not logged in
  resotone seed         insert the explainers demo dataset (RESONANS_SEED_TENANT) and exit
  resotone sanitize-backfill [--apply]
                        clean stored rich text in documents + document_versions
                        (dry-run unless --apply; as migrator role)
  resotone slug-audit   list stored slugs that create now rejects (read-only; as migrator role)
  resotone healthcheck  probe /healthz on $PORT and exit 0 (ok) / 1 (fail)
  resotone version      print the build version
  resotone help         show this message`)
}
