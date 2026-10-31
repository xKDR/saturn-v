// Command saturn runs heavy Julia, R and Python jobs on AWS Fargate from your laptop.
//
// This is the single-file version of bin/saturn: it needs no AWS CLI, Python or other
// tools for signing in, running and following jobs, and costs. connect, disconnect and
// status (the mounts and the database tunnel) are still done by bin/saturn.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

var version = "dev" // set at build time: -ldflags "-X main.version=v0.1.0"

const usageText = `saturn: run heavy Julia, R and Python jobs on AWS Fargate from your laptop.

Usage:
  saturn login [--use-device-code]      # sign in (IAM Identity Center); lasts up to a week
  saturn run [-e NAME=VALUE ...] path/to/script.jl|.R|.py [args...]
  saturn logs <task-id>                 # follow a job's output again
  saturn stop <task-id>
  saturn list                           # running jobs
  saturn costs                          # spending per month and person
  saturn cost <task-id>                 # (re)compute one job's cost
  saturn version

The script path is relative to the workspace (~/aws/workspace on your laptop,
/workspace in the job). Julia: packages come from the nearest Project.toml. R: from
renv.lock in the script's folder, if there is one. Python: from the nearest
pyproject.toml, a requirements.txt next to the script, or the script's own header.

-e sets an environment variable inside the job (repeatable).
Size and options (env vars): CPU=16384 MEMORY=65536 (default 4 vCPU / 16 GB; up to
32768 / 249856), SPOT=1 (~70% cheaper, can be interrupted), DETACH=1 (return at once).
R jobs run on the R version (major.minor) of the renv.lock next to the script, or the
newest available; R_VERSION=4.5 chooses one.
Ctrl-C stops following a job; the job keeps running. Your laptop can sleep meanwhile.

connect, disconnect and status are done by the saturn script for now (bin/saturn).
`

// exitError ends the program with a code and, if set, a message.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

func fail(format string, a ...any) error { return exitError{1, fmt.Sprintf(format, a...)} }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, "saturn:", ee.msg)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "saturn:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return exitError{code: 2}
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "version", "--version":
		fmt.Println("saturn", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return nil
	case "login":
		return login(ctx, args)
	case "connect", "disconnect", "status":
		return fail("%s isn't in this saturn yet; use the saturn script (bin/saturn %s)", cmd, cmd)
	}

	s, err := newSession(ctx)
	if err != nil {
		return err
	}
	switch cmd {
	case "run":
		return s.runJob(ctx, args)
	case "logs":
		if len(args) != 1 {
			return fail("usage: saturn logs <task-id>")
		}
		return s.follow(ctx, args[0])
	case "stop":
		if len(args) != 1 {
			return fail("usage: saturn stop <task-id>")
		}
		return s.stopJob(ctx, args[0])
	case "list":
		return s.listJobs(ctx)
	case "cost":
		if len(args) != 1 {
			return fail("usage: saturn cost <task-id>")
		}
		_, err := s.recordCost(ctx, args[0])
		return err
	case "costs":
		return s.showCosts(ctx)
	}
	fmt.Fprint(os.Stderr, usageText)
	return exitError{code: 2}
}

// taskID accepts a task ID or a full task ARN.
func taskID(s string) string { return s[strings.LastIndex(s, "/")+1:] }
