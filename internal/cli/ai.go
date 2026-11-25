package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/app"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// aiEnv is what the ai commands work with. Runs are executed by a running
// `izci serve`; the CLI queues, inspects, cancels and publishes them.
type aiEnv struct {
	db     *db.DB
	runner *ai.Runner
}

func (a *cliApp) aiEnv(ctx context.Context) (*aiEnv, error) {
	database, err := a.openDB(ctx)
	if err != nil {
		return nil, err
	}
	box, err := secret.NewBox(a.cfg.SecretKey, a.cfg.SecretKeyPrevious.Bytes()...)
	if err != nil {
		database.Close()
		return nil, err
	}
	access, err := app.NewGitHubAccess(a.cfg, box)
	if err != nil {
		database.Close()
		return nil, err
	}
	runner := ai.New(database, a.log, ai.Options{Config: a.cfg.AI, DataDir: a.cfg.DataDir, PublicURL: a.cfg.PublicURL, GitHub: access})
	return &aiEnv{db: database, runner: runner}, nil
}

// orgID resolves --org; it may be omitted when only one organization exists.
func (e *aiEnv) orgID(ctx context.Context, slug string) (uuid.UUID, error) {
	var ids []uuid.UUID
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM organizations WHERE $1 = '' OR slug = lower($1) ORDER BY slug LIMIT 2`, slug)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	switch {
	case err != nil:
		return uuid.Nil, err
	case len(ids) == 0 && slug != "":
		return uuid.Nil, fmt.Errorf("organization %q not found", slug)
	case len(ids) != 1:
		return uuid.Nil, errors.New("several organizations exist; choose one with --org")
	}
	return ids[0], nil
}

// issueID resolves a short id ("KALEM-7") or UUID inside an organization.
func (e *aiEnv) issueID(ctx context.Context, orgID uuid.UUID, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	slug, seq, ok := ingest.ParseShortID(ref)
	if !ok {
		return uuid.Nil, fmt.Errorf("%q is not an issue id", ref)
	}
	var id uuid.UUID
	err := e.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT i.id FROM issues i JOIN projects p ON p.id = i.project_id
			WHERE p.slug = $1 AND i.short_seq = $2`, slug, seq).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("issue %s not found", strings.ToUpper(ref))
	}
	return id, err
}

// run finds a run by id or unique id prefix (at least 8 characters).
func (e *aiEnv) run(ctx context.Context, ref string) (*store.AiRun, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if len(ref) < 8 {
		return nil, errors.New("give at least 8 characters of the run id")
	}
	var runs []*store.AiRun
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM ai_runs WHERE id::text LIKE $1 || '%' LIMIT 2`, ref)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := store.GetAiRun(ctx, tx, id)
			if err != nil {
				return err
			}
			runs = append(runs, r)
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, err
	case len(runs) == 0:
		return nil, fmt.Errorf("run %s not found", ref)
	case len(runs) > 1:
		return nil, fmt.Errorf("run id %s is ambiguous; give more characters", ref)
	}
	return runs[0], nil
}

var runStatusNames = map[int16]string{store.AiQueued: "queued", store.AiPreparing: "preparing", store.AiRunning: "running",
	store.AiSucceeded: "succeeded", store.AiFailed: "failed", store.AiCancelled: "cancelled"}

var runTriggerNames = map[int16]string{store.AiTriggerManual: "web", store.AiTriggerAutomatic: "automatic", store.AiTriggerCLI: "cli",
	store.AiTriggerMCP: "mcp", store.AiTriggerTelegram: "telegram"}

var logKindNames = map[int16]string{ai.LogSystem: "izci", ai.LogText: "agent", ai.LogTool: "tool", ai.LogStep: "step", ai.LogError: "error"}

func printLogs(w io.Writer, logs []store.AiLog) {
	for _, l := range logs {
		label := logKindNames[l.Kind]
		if l.Tool != "" {
			label = l.Tool
		}
		fmt.Fprintf(w, "%s %-6s %s\n", l.Time.Local().Format("15:04:05"), label, strings.ReplaceAll(l.Message, "\n", "\n                "))
	}
}

// follow prints the log of a run until it finishes and returns the run.
func (e *aiEnv) follow(ctx context.Context, w io.Writer, run *store.AiRun) (*store.AiRun, error) {
	seq := 0
	for {
		var logs []store.AiLog
		err := e.db.InOrg(ctx, run.OrgID, func(tx pgx.Tx) error {
			var err error
			if logs, err = store.ListAiLogs(ctx, tx, run.ID, seq); err != nil {
				return err
			}
			run, err = store.GetAiRun(ctx, tx, run.ID)
			return err
		})
		if err != nil {
			return nil, err
		}
		printLogs(w, logs)
		if len(logs) > 0 {
			seq = logs[len(logs)-1].Seq
		}
		if !run.Active() {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func runOutcome(run *store.AiRun) error {
	switch run.Status {
	case store.AiSucceeded:
		return nil
	case store.AiCancelled:
		return errors.New("the run was cancelled")
	}
	return fmt.Errorf("the run failed: %s", run.Error)
}

func newAiCmd(a *cliApp) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Analyze issues with AI (opencode) and publish fixes",
		Long: `AI analyses run inside a running "izci serve" (AI_ENABLED=true): the server
checks out the linked repository at the failing release, runs opencode with
restricted permissions and records the analysis and the proposed change. These
commands queue and inspect runs and turn proposed changes into branches and
pull requests.`,
	}
	var org string
	cmd.PersistentFlags().StringVar(&org, "org", "", "organization slug (optional when only one exists)")

	var mode, model, instructions string
	var wait bool
	analyze := &cobra.Command{
		Use:   "analyze ISSUE",
		Short: "Queue an analysis of an issue (short id like KALEM-7, or UUID)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			orgID, err := e.orgID(ctx, org)
			if err != nil {
				return err
			}
			issueID, err := e.issueID(ctx, orgID, args[0])
			if err != nil {
				return err
			}
			var m int16
			switch mode {
			case "":
			case "fix":
				m = store.AiModeFix
			case "analyze":
				m = store.AiModeAnalyze
			default:
				return errors.New(`--mode must be "fix" or "analyze"`)
			}
			run, err := e.runner.Start(ctx, orgID, ai.StartRequest{IssueID: issueID, Mode: m, Model: model, Instructions: instructions,
				Trigger: store.AiTriggerCLI})
			switch {
			case errors.Is(err, ai.ErrDisabled):
				return errors.New("AI analysis is disabled; set AI_ENABLED=true (and check the setup with: izci ai doctor)")
			case err != nil:
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "run %s queued; a running izci server executes it\n", run.ID)
			if !wait {
				return nil
			}
			run, err = e.follow(ctx, out, run)
			if err != nil {
				return err
			}
			if run.Status == store.AiSucceeded {
				fmt.Fprintf(out, "\n%s\n", run.Analysis)
				if len(run.Files) > 0 {
					fmt.Fprintf(out, "\n%d files changed; publish with: izci ai branch %s  or  izci ai pr %s\n", len(run.Files), run.ID, run.ID)
				}
			}
			return runOutcome(run)
		},
	}
	analyze.Flags().StringVar(&mode, "mode", "", `"fix" (default when a repository is linked) or "analyze"`)
	analyze.Flags().StringVar(&model, "model", "", "opencode model (provider/model)")
	analyze.Flags().StringVar(&instructions, "instructions", "", "extra instructions for the agent")
	analyze.Flags().BoolVar(&wait, "wait", false, "follow the log until the run finishes")

	var issue, status string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List recent runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			orgID, err := e.orgID(ctx, org)
			if err != nil {
				return err
			}
			f := store.AiRunFilter{}
			if issue != "" {
				id, err := e.issueID(ctx, orgID, issue)
				if err != nil {
					return err
				}
				f.IssueID = &id
			}
			for code, name := range runStatusNames {
				if name == status {
					f.Status = code
				}
			}
			if status != "" && f.Status == 0 {
				return fmt.Errorf("unknown status %q", status)
			}
			var runs []*store.AiRun
			short := map[uuid.UUID]string{}
			err = e.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
				if runs, err = store.ListAiRuns(ctx, tx, orgID, f, limit); err != nil {
					return err
				}
				rows, err := tx.Query(ctx, `SELECT i.id, p.slug, coalesce(i.short_seq, 0) FROM issues i JOIN projects p ON p.id = i.project_id
					WHERE i.id IN (SELECT issue_id FROM ai_runs WHERE org_id = $1)`, orgID)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var id uuid.UUID
					var slug string
					var seq int32
					if err := rows.Scan(&id, &slug, &seq); err != nil {
						return err
					}
					short[id] = ingest.ShortID(slug, seq)
				}
				return rows.Err()
			})
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN\tISSUE\tSTATUS\tTRIGGER\tFILES\tCREATED")
			for _, r := range runs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n", r.ID.String()[:13], short[r.IssueID], runStatusNames[r.Status],
					runTriggerNames[r.Trigger], len(r.Files), r.CreatedAt.Local().Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	list.Flags().StringVar(&issue, "issue", "", "only runs of this issue")
	list.Flags().StringVar(&status, "status", "", "queued, preparing, running, succeeded, failed or cancelled")
	list.Flags().IntVar(&limit, "limit", 20, "maximum runs to show")

	var showDiff bool
	show := &cobra.Command{
		Use:   "show RUN",
		Short: "Show a run's result, analysis and (with --diff) the proposed change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			r, err := e.run(ctx, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "run       %s\nstatus    %s\ntrigger   %s\nmodel     %s\n", r.ID, runStatusNames[r.Status], runTriggerNames[r.Trigger], r.Model)
			if r.BaseCommit != "" {
				fmt.Fprintf(out, "commit    %s (%s)\n", r.BaseCommit, r.BaseBranch)
			}
			if r.Usage.InputTokens > 0 {
				fmt.Fprintf(out, "tokens    %d in, %d out, $%.4f\n", r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.CostUSD)
			}
			if r.PullRequest != nil {
				fmt.Fprintf(out, "published %s\n", r.PullRequest.URL)
			}
			if r.Error != "" {
				fmt.Fprintf(out, "error     %s\n", r.Error)
			}
			for _, f := range r.Files {
				fmt.Fprintf(out, "file      %s (%s, +%d -%d)\n", f.Path, f.Status, f.Additions, f.Deletions)
			}
			if r.Analysis != "" {
				fmt.Fprintf(out, "\n%s\n", r.Analysis)
			}
			if showDiff && r.Diff != "" {
				fmt.Fprintf(out, "\n%s", r.Diff)
			}
			return nil
		},
	}
	show.Flags().BoolVar(&showDiff, "diff", false, "print the proposed change")

	var followLog bool
	logs := &cobra.Command{
		Use:   "logs RUN",
		Short: "Print a run's log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			r, err := e.run(ctx, args[0])
			if err != nil {
				return err
			}
			if followLog {
				_, err = e.follow(ctx, cmd.OutOrStdout(), r)
				return err
			}
			return e.db.InOrg(ctx, r.OrgID, func(tx pgx.Tx) error {
				l, err := store.ListAiLogs(ctx, tx, r.ID, 0)
				printLogs(cmd.OutOrStdout(), l)
				return err
			})
		},
	}
	logs.Flags().BoolVarP(&followLog, "follow", "f", false, "keep printing until the run finishes")

	var branchName string
	var allowSensitive bool
	branch := &cobra.Command{
		Use:   "branch RUN",
		Short: "Push the proposed change of a run to a new branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			r, err := e.run(ctx, args[0])
			if err != nil {
				return err
			}
			info, err := e.runner.CreateBranch(ctx, r.OrgID, r.ID, ai.BranchOptions{Name: branchName, AllowSensitive: allowSensitive}, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "branch %s: %s\n", info.Branch, info.URL)
			return nil
		},
	}
	branch.Flags().StringVar(&branchName, "name", "", "branch name (default: izci/fix/<issue>-<title>-<run>)")
	branch.Flags().BoolVar(&allowSensitive, "allow-sensitive", false,
		"push even though the change touches files CI, build tools or git run (review them first)")

	var draft bool
	var title, base string
	pr := &cobra.Command{
		Use:   "pr RUN",
		Short: "Push the proposed change and open a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			r, err := e.run(ctx, args[0])
			if err != nil {
				return err
			}
			info, err := e.runner.CreatePullRequest(ctx, r.OrgID, r.ID,
				ai.PullRequestOptions{Draft: draft, Title: title, Base: base, AllowSensitive: allowSensitive}, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pull request #%d: %s\n", info.Number, info.URL)
			return nil
		},
	}
	pr.Flags().BoolVar(&draft, "draft", true, "open as a draft")
	pr.Flags().StringVar(&title, "title", "", "title (default: fix: <issue title> (<short id>))")
	pr.Flags().StringVar(&base, "base", "", "target branch (default: the branch the analysis ran on)")
	pr.Flags().BoolVar(&allowSensitive, "allow-sensitive", false,
		"push even though the change touches files CI, build tools or git run (review them first)")

	cancel := &cobra.Command{
		Use:   "cancel RUN",
		Short: "Stop a queued or running run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.aiEnv(ctx)
			if err != nil {
				return err
			}
			defer e.db.Close()
			r, err := e.run(ctx, args[0])
			if err != nil {
				return err
			}
			if !r.Active() {
				return fmt.Errorf("run %s already %s", r.ID, runStatusNames[r.Status])
			}
			if _, err := e.runner.Cancel(ctx, r.OrgID, r.ID, "cancelled from the CLI"); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "run %s cancelled\n", r.ID)
			return nil
		},
	}

	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "Check what AI analyses need (opencode, model, git, data directory, GitHub)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.load(); err != nil {
				return err
			}
			box, err := secret.NewBox(a.cfg.SecretKey, a.cfg.SecretKeyPrevious.Bytes()...)
			if err != nil {
				return err
			}
			access, err := app.NewGitHubAccess(a.cfg, box)
			if err != nil {
				return err
			}
			runner := ai.New(nil, a.log, ai.Options{Config: a.cfg.AI, DataDir: a.cfg.DataDir, PublicURL: a.cfg.PublicURL, GitHub: access})
			failed := false
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			for _, c := range runner.Doctor(cmd.Context()) {
				mark := "ok"
				switch {
				case !c.OK && c.Optional:
					mark = "--"
				case !c.OK:
					mark, failed = "FAIL", true
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", mark, c.Name, c.Detail)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if failed {
				return errors.New("AI analysis is not ready")
			}
			return nil
		},
	}

	cmd.AddCommand(analyze, list, show, logs, branch, pr, cancel, doctor)
	return cmd
}
