package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/tui"
)

// --- credentials -------------------------------------------------------------------

// account is a host with whatever credentials this machine has for it.
type account struct {
	host   string
	hosts  *cloud.Hosts
	config *cloud.HostConfig // nil when not logged in (or only via env)
	client *cloud.Client
}

func (a *account) name() string { return cloud.HostName(a.host) }

// openAccount loads credentials for host ($TODOMD_TOKEN wins over the
// stored token). With needToken, a missing token is cloud.ErrNotLoggedIn.
func openAccount(host string, needToken bool) (*account, error) {
	hosts, err := cloud.LoadHosts()
	if err != nil {
		return nil, err
	}
	a := &account{host: host, hosts: hosts, config: hosts.Hosts[host]}
	token, api := os.Getenv("TODOMD_TOKEN"), ""
	if a.config != nil {
		api = a.config.API
		if token == "" {
			token = a.config.Token
		}
	}
	if needToken && token == "" {
		return nil, notLoggedIn(host)
	}
	a.client = cloud.New(host, api, token, resolveVersion())
	return a, nil
}

func notLoggedIn(host string) error {
	flag := ""
	if host != cloud.DefaultHost {
		flag = " --host " + host
	}
	return fmt.Errorf("%w to %s (run 'todomd login%s')", cloud.ErrNotLoggedIn, cloud.HostName(host), flag)
}

func formatVersion(a *account) int {
	if a.config != nil {
		return a.config.FormatVersion
	}
	return 0
}

func cmdContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// --- login / logout / whoami ---------------------------------------------------------

func newLogin() *cobra.Command {
	var hostFlag string
	var noBrowser, withToken bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to todomd.online (or another host) from this machine",
		Long: `Log in with the device flow: todomd prints a short code and a URL, and you
approve this machine in any browser — signing in there with GitHub or an
emailed code, and signing up if you're new. Works over ssh and in containers.

Agents don't log in themselves: they use this machine's login, or a token
scoped to one project (made on the web) via TODOMD_TOKEN or --with-token.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := cloud.ResolveHost(hostFlag)
			if err != nil {
				return err
			}
			a, err := openAccount(host, false)
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext()
			defer cancel()
			c := cloud.New(host, "", "", resolveVersion())
			disc, err := c.Discover(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", a.name(), err)
			}

			var tok *cloud.Token
			if withToken {
				data, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
				if err != nil {
					return err
				}
				tok = &cloud.Token{Token: strings.TrimSpace(string(data))}
				if tok.Token == "" {
					return errors.New("--with-token: no token on stdin")
				}
			} else {
				if tok, err = deviceLogin(ctx, c, a.name(), noBrowser); err != nil {
					return err
				}
			}
			c.Token = tok.Token
			me, err := c.Me(ctx)
			if err != nil {
				return err
			}
			if old := a.config; old != nil && old.Token != "" && old.Token != tok.Token {
				oc := cloud.New(host, old.API, old.Token, resolveVersion())
				_ = oc.RevokeToken(ctx) // replaced: best effort
			}
			a.hosts.Hosts[host] = &cloud.HostConfig{
				API: c.API, Token: tok.Token, TokenID: me.Token.ID, Kind: me.Token.Kind, User: me.User,
				CreatedAt: time.Now().UTC(), MinCLIVersion: disc.MinCLIVersion, FormatVersion: disc.FormatVersion,
			}
			if err := a.hosts.Save(); err != nil {
				return err
			}
			who := me.User.Handle
			if who == "" {
				who = me.User.Email
			} else if me.User.Email != "" {
				who += " (" + me.User.Email + ")"
			}
			fmt.Printf("Logged in to %s as %s\n", a.name(), who)
			if me.User.Handle == "" {
				fmt.Printf("Pick a handle to finish signing up: %s/welcome\n", host)
			}
			if os.Getenv("TODOMD_TOKEN") != "" {
				fmt.Fprintln(os.Stderr, "todomd: note: TODOMD_TOKEN is set and takes precedence over this login")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&hostFlag, "host", "", "service to log in to (default https://todomd.online; env TODOMD_HOST)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the URL, don't open a browser")
	cmd.Flags().BoolVar(&withToken, "with-token", false, "read a token (e.g. a scoped one made on the web) from stdin instead")
	return cmd
}

func deviceName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return strings.TrimSuffix(h, ".local")
}

func deviceLogin(ctx context.Context, c *cloud.Client, hostName string, noBrowser bool) (*cloud.Token, error) {
	dc, err := c.StartDevice(ctx, deviceName(), resolveVersion())
	if err != nil {
		return nil, err
	}
	fmt.Printf("! First copy your one-time code: %s\n", dc.UserCode)
	opened := false
	if !noBrowser && interactive() && dc.VerificationURIComplete != "" {
		opened = openBrowser(dc.VerificationURIComplete)
	}
	if opened {
		fmt.Printf("  Opened %s in your browser — approve this machine there.\n", dc.VerificationURI)
	} else {
		fmt.Printf("  Open %s and enter it.\n", dc.VerificationURI)
	}
	fmt.Print("  Waiting for approval…")
	interval := time.Duration(max(dc.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(dc.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			fmt.Println()
			return nil, ctx.Err()
		}
		tok, err := c.PollDevice(ctx, dc.DeviceCode)
		var de *cloud.DeviceError
		if errors.As(err, &de) {
			switch de.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			case "access_denied":
				fmt.Println()
				return nil, errors.New("login was denied in the browser")
			case "expired_token":
				fmt.Println()
				return nil, errors.New("the code expired or was already used; run 'todomd login' again")
			}
			fmt.Println()
			return nil, fmt.Errorf("login failed: %s", de.Code)
		}
		var ue *cloud.UnavailableError
		if errors.As(err, &ue) {
			continue // a blip while waiting; keep polling until the deadline
		}
		if err != nil {
			fmt.Println()
			return nil, err
		}
		fmt.Println(" ✓")
		return tok, nil
	}
	fmt.Println()
	return nil, errors.New("the code expired; run 'todomd login' again")
}

func newLogout() *cobra.Command {
	var hostFlag string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke this machine's login and forget it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := cloud.ResolveHost(hostFlag)
			if err != nil {
				return err
			}
			a, err := openAccount(host, false)
			if err != nil {
				return err
			}
			if a.config == nil {
				fmt.Printf("not logged in to %s\n", a.name())
				return nil
			}
			ctx, cancel := cmdContext()
			defer cancel()
			c := cloud.New(host, a.config.API, a.config.Token, resolveVersion())
			if err := c.RevokeToken(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "todomd: warning: couldn't revoke the token on %s (%v); it is forgotten here anyway\n", a.name(), err)
			}
			delete(a.hosts.Hosts, host)
			if err := a.hosts.Save(); err != nil {
				return err
			}
			fmt.Printf("Logged out of %s\n", a.name())
			if os.Getenv("TODOMD_TOKEN") != "" {
				fmt.Fprintln(os.Stderr, "todomd: note: TODOMD_TOKEN is still set and still works")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&hostFlag, "host", "", "service to log out of (default https://todomd.online; env TODOMD_HOST)")
	return cmd
}

func tokenPrefix(t string) string {
	if len(t) > 8 {
		return t[:8] + "…"
	}
	return t
}

func newWhoami() *cobra.Command {
	var hostFlag string
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show who this machine is logged in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := cloud.ResolveHost(hostFlag)
			if err != nil {
				return err
			}
			a, err := openAccount(host, true)
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext()
			defer cancel()
			me, err := a.client.Me(ctx)
			if err != nil {
				return err
			}
			source := "login"
			if os.Getenv("TODOMD_TOKEN") != "" {
				source = "TODOMD_TOKEN"
			}
			if flagJSON {
				return printJSON(map[string]any{
					"host": host, "user": me.User, "token": map[string]any{
						"id": me.Token.ID, "kind": me.Token.Kind, "scopes": me.Token.Scopes,
						"prefix": tokenPrefix(a.client.Token), "source": source,
					},
				})
			}
			who := me.User.Handle
			if who == "" {
				who = "(no handle yet)"
			}
			fmt.Printf("host:   %s\nuser:   %s", a.name(), who)
			if me.User.Email != "" {
				fmt.Printf(" (%s)", me.User.Email)
			}
			fmt.Printf("\ntoken:  %s %s, from %s\n", me.Token.Kind, tokenPrefix(a.client.Token), source)
			for _, s := range me.Token.Scopes {
				fmt.Printf("scope:  %s (%s)\n", s.Slug, s.Permission)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&hostFlag, "host", "", "service (default https://todomd.online; env TODOMD_HOST)")
	jsonFlag(cmd)
	return cmd
}

// --- link / unlink ---------------------------------------------------------------------

// linkTarget is the todo file to link: the one todomd finds, or ./TODO.md
// when there is none yet (link can create it from the project).
func linkTarget() (*store.Store, error) {
	s, err := newStore(false)
	if errors.Is(err, store.ErrNoFile) {
		return newStore(true)
	}
	return s, err
}

var nameClean = regexp.MustCompile(`[^a-z0-9-]+`)

// defaultProjectName derives a valid project name ([a-z0-9-], 2–39) from the
// repo or the directory holding the file.
func defaultProjectName(repo, path string) string {
	base := filepath.Base(filepath.Dir(path))
	if repo != "" {
		base = repo[strings.LastIndex(repo, "/")+1:]
	}
	n := strings.Trim(nameClean.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(n) > 39 {
		n = strings.Trim(n[:39], "-")
	}
	if len(n) < 2 {
		n = "todo"
	}
	return n
}

type linkOpts struct {
	hostFlag, project, name, region, git string
	create, replaceLocal, replaceRemote  bool
	yes                                  bool
}

func newLink() *cobra.Command {
	var o linkOpts
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Link this TODO.md to a todomd.online project",
		Long: `Link the current todo file to a project on todomd.online, creating the
project or picking an existing one (projects whose repo matches this git
remote come first). Linking to a project that already has content merges
both — a preview is shown first. With no local file, the project's board is
written to ./TODO.md.

Inside a git repository, link also asks whether TODO.md should stay in git:
once synced, todomd.online keeps its history, and a file in both turns branch
switches into edits. The recommended answer stops tracking it (added to
.gitignore, removed from the index only — commit that yourself).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := cmdContext()
			defer cancel()
			_, err := runLink(ctx, o)
			return err
		},
	}
	cmd.Flags().StringVar(&o.hostFlag, "host", "", "service (default https://todomd.online; env TODOMD_HOST)")
	cmd.Flags().StringVar(&o.project, "project", "", "link to this project (owner/name or id)")
	cmd.Flags().BoolVar(&o.create, "create", false, "create a new project from this file")
	cmd.Flags().StringVar(&o.name, "name", "", "name for --create (default: the repo or directory name)")
	cmd.Flags().StringVar(&o.region, "region", "", "data location for --create: earth, eu or us (default: your account's)")
	cmd.Flags().BoolVar(&o.replaceLocal, "replace-local", false, "overwrite this file with the project instead of merging")
	cmd.Flags().BoolVar(&o.replaceRemote, "replace-remote", false, "make this file the project's new head instead of merging (history keeps the old one)")
	cmd.Flags().StringVar(&o.git, "git", "", "keep TODO.md in git: ignore (stop tracking, recommended) or track")
	cmd.Flags().BoolVar(&o.yes, "yes", false, "don't ask; required off a terminal for a merge preview or a single repo match")
	jsonFlag(cmd)
	return cmd
}

func runLink(ctx context.Context, o linkOpts) (*cloudsync.Syncer, error) {
	if o.replaceLocal && o.replaceRemote {
		return nil, errors.New("--replace-local and --replace-remote exclude each other")
	}
	if o.git != "" && o.git != cloudsync.GitIgnore && o.git != cloudsync.GitTrack {
		return nil, errors.New("--git must be ignore or track")
	}
	st, err := linkTarget()
	if err != nil {
		return nil, err
	}
	if l, err := cloudsync.LoadLink(st.Path); err == nil {
		return nil, fmt.Errorf("%s is already linked to %s (run 'todomd unlink' first)", st.Path, l.ProjectSlug)
	}
	host, err := cloud.ResolveHost(o.hostFlag)
	if err != nil {
		return nil, err
	}
	a, err := openAccount(host, true)
	if err != nil {
		return nil, err
	}
	s := &cloudsync.Syncer{Path: st.Path, Store: st, Client: a.client, Link: &cloudsync.Link{Host: host},
		Version: resolveVersion(), FormatVersion: formatVersion(a)}
	repo := cloudsync.OriginRepo(st.Path)
	tty := interactive() && !flagJSON

	// Which project.
	var project *cloud.Project
	switch {
	case o.create:
	case o.project != "":
		if project, err = a.client.Project(ctx, o.project); err != nil {
			if cloud.IsStatus(err, 404, "") {
				return nil, fmt.Errorf("no project %q on %s that you can see", o.project, a.name())
			}
			return nil, err
		}
	default:
		var matches []cloud.Project
		if repo != "" {
			if matches, err = a.client.Projects(ctx, repo); err != nil {
				return nil, err
			}
		}
		if !tty {
			if len(matches) == 1 && o.yes {
				project = &matches[0]
				break
			}
			return nil, &linkChoiceError{repo: repo, matches: matches}
		}
		all, err := a.client.Projects(ctx, "")
		if err != nil {
			return nil, err
		}
		var options []string
		var picks []*cloud.Project
		seen := map[string]bool{}
		for i := range matches {
			options = append(options, matches[i].Slug+"  (this repo)")
			picks = append(picks, &matches[i])
			seen[matches[i].ID] = true
		}
		options = append(options, "Create a new project")
		picks = append(picks, nil)
		for i := range all {
			if !seen[all[i].ID] {
				options = append(options, all[i].Slug)
				picks = append(picks, &all[i])
			}
		}
		fmt.Printf("%s isn't linked to a %s project yet.\n", filepath.Base(st.Path), a.name())
		k := choose("Link to:", options)
		if k < 0 {
			return nil, errors.New("cancelled")
		}
		project = picks[k]
		if project == nil {
			o.create = true
		}
	}

	// How the file relates to git, decided before anything changes.
	inRepo := cloudsync.RepoRoot(st.Path) != ""
	gitMode := ""
	if inRepo {
		gitMode = o.git
		if gitMode == "" {
			gitMode = cloudsync.GitIgnore
			if cloudsync.IsTracked(st.Path) {
				switch {
				case tty:
					k := choose(filepath.Base(st.Path)+" is tracked by git. Keep it in git too?", []string{
						a.name() + " keeps the history: stop tracking it (recommended)",
						"Keep it in git as well",
					})
					if k == 1 {
						gitMode = cloudsync.GitTrack
					}
				default:
					gitMode = cloudsync.GitTrack
					fmt.Fprintf(os.Stderr, "todomd: warning: %s stays tracked by git as well (pass --git ignore to stop tracking it, or --git track to silence this)\n", filepath.Base(st.Path))
				}
			}
		}
	}
	s.Link.Git = gitMode

	if o.create {
		name := o.name
		if name == "" {
			name = defaultProjectName(repo, st.Path)
			if tty {
				name = ask("Project name", name)
			}
		}
		region := o.region
		created, err := s.CreateProject(ctx, name, repo, region)
		if err != nil {
			return nil, createError(err, host)
		}
		s.Link.Git = gitMode
		if err := s.Link.Save(st.Path); err != nil {
			return nil, err
		}
		if err := applyGitMode(st.Path, gitMode); err != nil {
			return s, err
		}
		if err := offerWebImages(ctx, s, o.yes, tty); err != nil {
			return s, err
		}
		if flagJSON {
			return s, printJSON(linkJSON(s, created.Rev, nil))
		}
		fmt.Printf("Created %s · %s\nLinked %s · rev %d\n", created.Slug, created.URL, st.Path, created.Rev)
		return s, nil
	}

	*s.Link = *cloudsync.NewLink(host, *project)
	s.Link.Git = gitMode
	mode := cloudsync.LinkMerge
	if o.replaceLocal {
		mode = cloudsync.LinkReplaceLocal
	} else if o.replaceRemote {
		mode = cloudsync.LinkReplaceRemote
	}
	if !o.yes {
		p, err := s.PreviewFirst(ctx, mode)
		if err != nil {
			return nil, err
		}
		if !tty {
			if !p.Missing && (len(p.FromOnline.Pulled) > 0 || p.FromFile > 0 || mode != cloudsync.LinkMerge) {
				return nil, fmt.Errorf("linking would change this file or %s (%d change(s) from online, %d from this file); pass --yes to go ahead",
					project.Slug, len(p.FromOnline.Pulled), p.FromFile)
			}
		} else {
			fmt.Printf("Linking %s to %s (rev %d). Preview:\n", filepath.Base(st.Path), project.Slug, p.HeadRev)
			if p.Missing {
				fmt.Printf("  no local file: it will be created from the project\n")
			}
			fmt.Printf("  from online:    %s\n", countEvents(p.FromOnline.Pulled))
			if mode == cloudsync.LinkReplaceLocal {
				fmt.Printf("  this file is replaced by the project\n")
			} else {
				fmt.Printf("  from this file: %s\n", countN(p.FromFile))
			}
			if n := len(p.FromOnline.Conflicts); n > 0 {
				fmt.Printf("  conflicts:      %d (online wins; this file's version is kept as a comment)\n", n)
			}
			verb := "Merge and link?"
			if mode != cloudsync.LinkMerge {
				verb = "Replace and link?"
			}
			if !confirmYes(verb) {
				return nil, errors.New("cancelled")
			}
		}
	}
	res, err := s.FirstLink(ctx, mode)
	if err != nil {
		return nil, err
	}
	if err := applyGitMode(st.Path, gitMode); err != nil {
		return s, err
	}
	if err := offerWebImages(ctx, s, o.yes, tty); err != nil {
		return s, err
	}
	if flagJSON {
		return s, printJSON(linkJSON(s, res.Rev, res))
	}
	fmt.Printf("Linked %s to %s · rev %d · %s\n", st.Path, project.Slug, res.Rev, project.URL)
	printConflicts(res)
	return s, nil
}

// offerWebImages brings images attached in todomd-web over into todomd's
// folder, so they sync, and pushes them right away. It asks first (or needs
// --yes off a terminal).
func offerWebImages(ctx context.Context, s *cloudsync.Syncer, yes, tty bool) error {
	f, err := s.Store.Load()
	if err != nil {
		return nil
	}
	n := cloudsync.WebImages(f)
	if n == 0 {
		return nil
	}
	out := os.Stdout
	if flagJSON {
		out = os.Stderr
	}
	switch {
	case tty:
		if !confirmYes(fmt.Sprintf("This file links to %d image(s) attached in todomd-web. Copy them so they sync?", n)) {
			return nil
		}
	case !yes:
		fmt.Fprintf(out, "This file links to %d image(s) attached in todomd-web; run 'todomd link' on a terminal (or with --yes) to bring them over.\n", n)
		return nil
	}
	moved, err := cloudsync.AdoptWebImages(s.Store, s.Link)
	if err != nil {
		return err
	}
	res, err := s.Round(ctx, cloudsync.Options{})
	if err != nil {
		return fmt.Errorf("brought over %d image link(s), but the sync that uploads them failed: %w", moved, err)
	}
	fmt.Fprintf(out, "Brought over %d image link(s) from todomd-web; uploaded %d image(s).\n", moved, len(res.Attachments.Uploaded))
	return nil
}

func countN(n int) string {
	if n == 0 {
		return "nothing new"
	}
	return fmt.Sprintf("%d change(s)", n)
}

func countEvents(evs []changes.Event) string {
	if len(evs) == 0 {
		return "nothing new"
	}
	added, other := 0, 0
	for _, e := range evs {
		if e.Type == changes.TaskAdded {
			added++
		} else {
			other++
		}
	}
	parts := []string{}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d task(s)", added))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d other change(s)", other))
	}
	return strings.Join(parts, ", ")
}

type linkChoiceError struct {
	repo    string
	matches []cloud.Project
}

func (e *linkChoiceError) Error() string {
	switch len(e.matches) {
	case 0:
		if e.repo == "" {
			return "no project to link to: pass --project owner/name, or --create"
		}
		return fmt.Sprintf("no project for %s yet: pass --create, or --project owner/name", e.repo)
	case 1:
		return fmt.Sprintf("%s matches this repo: pass --yes to link to it, or --project/--create", e.matches[0].Slug)
	}
	var slugs []string
	for _, p := range e.matches {
		slugs = append(slugs, p.Slug)
	}
	return fmt.Sprintf("several projects match this repo (%s): pass --project owner/name", strings.Join(slugs, ", "))
}

func createError(err error, host string) error {
	var ae *cloud.APIError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Code {
	case "needs_handle":
		return fmt.Errorf("pick a handle first to finish signing up: %s/welcome", host)
	case "plan_limit":
		if u, ok := ae.Extra["url"].(string); ok {
			return fmt.Errorf("%s (%s)", ae.Message, u)
		}
	case "forbidden":
		return fmt.Errorf("this token can't create projects (scoped tokens can only reach their projects): %s", ae.Message)
	}
	return err
}

// applyGitMode makes the repository match the link's git mode (spec §12.1).
func applyGitMode(path, mode string) error {
	if mode != cloudsync.GitIgnore {
		return nil
	}
	indexChanged, err := cloudsync.Untrack(path)
	if err != nil {
		return fmt.Errorf("linked, but couldn't stop git tracking %s: %w", filepath.Base(path), err)
	}
	out := os.Stdout
	if flagJSON {
		out = os.Stderr
	}
	fmt.Fprintf(out, "Added %s to .gitignore.\n", filepath.Base(path))
	if indexChanged {
		fmt.Fprintf(out, "Removed %s from git's index (the file itself is untouched). Commit that change.\n"+
			"Note: when others pull it, git deletes their copy of %s; linked clones get it back on their next\n"+
			"'todomd sync', others with 'todomd link'.\n", filepath.Base(path), filepath.Base(path))
	}
	return nil
}

func linkJSON(s *cloudsync.Syncer, rev int, res *cloudsync.Result) map[string]any {
	out := map[string]any{
		"file":    s.Path,
		"project": map[string]any{"id": s.Link.ProjectID, "slug": s.Link.ProjectSlug, "url": s.Link.ProjectURL},
		"rev":     rev,
		"git":     s.Link.Git,
	}
	if res != nil {
		out["pulled"] = eventsJSON(res.Pulled)
		out["conflicts"] = conflictsOf(res)
		out["renamed_ids"] = renamedOf(res)
	}
	return out
}

func newUnlink() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Forget this file's link to its todomd.online project",
		Long:  "Removes the local link only: the file and the online project stay as they are.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := syncStore()
			if err != nil {
				return err
			}
			if err := cloudsync.Unlink(s.Path); err != nil {
				return err
			}
			fmt.Printf("unlinked %s\n", s.Path)
			return nil
		},
	}
}

// --- sync ---------------------------------------------------------------------------------

// syncStore finds the todo file for the sync commands. It is newStore, plus
// one case: when no TODO.md exists it accepts a linked path whose file is
// gone (git deleted it on a pull, say), so that sync can restore it.
func syncStore() (*store.Store, error) {
	s, err := newStore(false)
	if !errors.Is(err, store.ErrNoFile) {
		return s, err
	}
	dir, werr := os.Getwd()
	if werr != nil {
		return nil, err
	}
	for {
		p := filepath.Join(dir, store.DefaultFileName)
		if _, lerr := cloudsync.LoadLink(p); lerr == nil {
			return &store.Store{Path: p}, nil
		}
		if _, serr := os.Stat(filepath.Join(dir, ".git")); serr == nil {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, err
		}
		dir = parent
	}
}

// openSyncer builds the syncer for the current file's link.
func openSyncer() (*cloudsync.Syncer, *account, error) {
	st, err := syncStore()
	if err != nil {
		return nil, nil, err
	}
	link, err := cloudsync.LoadLink(st.Path)
	if err != nil {
		return nil, nil, err
	}
	a, err := openAccount(link.Host, true)
	if err != nil {
		return nil, nil, err
	}
	return &cloudsync.Syncer{Path: st.Path, Store: st, Client: a.client, Link: link,
		Version: resolveVersion(), FormatVersion: formatVersion(a)}, a, nil
}

func eventsJSON(evs []changes.Event) []eventJSON {
	out := []eventJSON{}
	for _, e := range evs {
		out = append(out, toEventJSON(e))
	}
	return out
}

func conflictsOf(r *cloudsync.Result) []merge.Conflict {
	if r.Conflicts == nil {
		return []merge.Conflict{}
	}
	return r.Conflicts
}

func opsOf(list []ops.Op) []ops.Op {
	if list == nil {
		return []ops.Op{}
	}
	return list
}

func renamedOf(r *cloudsync.Result) []merge.Rename {
	if r.Renamed == nil {
		return []merge.Rename{}
	}
	return r.Renamed
}

type syncJSON struct {
	File        string                     `json:"file"`
	Project     map[string]any             `json:"project"`
	Rev         int                        `json:"rev"`
	Pushed      []eventJSON                `json:"pushed"`
	Pulled      []eventJSON                `json:"pulled"`
	PushedBoard []ops.Op                   `json:"pushed_board_ops"`
	PulledBoard []ops.Op                   `json:"pulled_board_ops"`
	Conflicts   []merge.Conflict           `json:"conflicts"`
	RenamedIDs  []merge.Rename             `json:"renamed_ids"`
	Unchanged   bool                       `json:"unchanged"`
	DryRun      bool                       `json:"dry_run,omitempty"`
	ReadOnly    bool                       `json:"read_only,omitempty"`
	FullFile    bool                       `json:"full_file,omitempty"`
	Restored    bool                       `json:"restored,omitempty"`
	Import      bool                       `json:"import,omitempty"`
	Note        string                     `json:"note,omitempty"`
	Attachments cloudsync.AttachmentReport `json:"attachments"`
}

func toSyncJSON(s *cloudsync.Syncer, r *cloudsync.Result) syncJSON {
	return syncJSON{
		File:        s.Path,
		Project:     map[string]any{"id": s.Link.ProjectID, "slug": s.Link.ProjectSlug, "url": s.Link.ProjectURL},
		Rev:         r.Rev,
		Pushed:      eventsJSON(r.Pushed),
		Pulled:      eventsJSON(r.Pulled),
		PushedBoard: opsOf(r.PushedBoards),
		PulledBoard: opsOf(r.PulledBoards),
		Conflicts:   conflictsOf(r),
		RenamedIDs:  renamedOf(r),
		Unchanged:   r.Unchanged,
		DryRun:      r.DryRun,
		ReadOnly:    r.ReadOnly,
		FullFile:    r.FullFile,
		Restored:    r.Restored,
		Import:      r.Import,
		Note:        r.Note,
		Attachments: attachmentsOf(r),
	}
}

func attachmentsOf(r *cloudsync.Result) cloudsync.AttachmentReport {
	a := r.Attachments
	if a.Uploaded == nil {
		a.Uploaded = []string{}
	}
	if a.Downloaded == nil {
		a.Downloaded = []string{}
	}
	if a.Skipped == nil {
		a.Skipped = []cloudsync.Skip{}
	}
	return a
}

func printConflicts(r *cloudsync.Result) {
	for _, c := range r.Conflicts {
		where := c.Task
		if where == "" {
			where = c.Board
		}
		if where == "" {
			where = "file"
		}
		field := c.Field
		if field == "" {
			field = c.Kind
		}
		note := c.Note
		if i := strings.IndexByte(note, '\n'); i >= 0 {
			note = note[:i] + " …"
		}
		fmt.Printf("  conflict %s %s: kept %s — %s\n", where, field, c.Kept, note)
	}
	for _, rn := range r.Renamed {
		fmt.Printf("  renamed %s → %s (%q): its id collided with a task added online\n", rn.From, rn.To, rn.Title)
	}
	for _, sk := range r.Attachments.Skipped {
		fmt.Printf("  image %s/%s left as it is: %s\n", sk.ID, sk.Name, sk.Reason)
	}
	if r.Note != "" {
		fmt.Fprintf(os.Stderr, "todomd: note: %s\n", r.Note)
	}
}

func newSync() *cobra.Command {
	var dryRun, watch, noAttachments bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync this TODO.md with its todomd.online project (push and pull)",
		Long: `Push this file's changes and pull the project's in one round, merged per
task and field. When both sides changed the same thing, the online version
wins and this file's version is kept as a comment by todomd-sync — nothing
is lost. Conflicts are reported, not errors.

Images travel too: an image linked from this machine's attachment folder
(see 'todomd attach') is uploaded when the project lacks it, and images added
online are downloaded and linked by local path, so agents can open them.

--watch keeps running: online edits arrive within a second or two and local
edits are pushed as they happen. Online changes land in the file like any
other edit, so 'todomd changes', the TUI's badges and todomd-web see them.

Exit codes: 4 not logged in, 5 not linked (or the project is gone),
6 todomd.online unreachable (safe to retry), 7 this todomd is too old.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := cmdContext()
			defer cancel()
			s, a, err := openSyncer()
			if errors.Is(err, cloudsync.ErrNotLinked) && interactive() && !flagJSON && !dryRun {
				if s, err = runLink(ctx, linkOpts{}); err != nil {
					return err
				}
				if a, err = openAccount(s.Link.Host, true); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			s.NoAttachments = noAttachments
			if watch {
				return runWatch(ctx, s, a)
			}
			res, err := s.Round(ctx, cloudsync.Options{DryRun: dryRun})
			if err != nil {
				if !dryRun {
					s.RecordError(err)
				}
				return syncError(err)
			}
			if flagJSON {
				return printJSON(toSyncJSON(s, res))
			}
			prefix := "Synced: "
			if dryRun {
				prefix = "Would sync: "
			}
			fmt.Println(prefix + cloudsync.Describe(res))
			printConflicts(res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what a round would do without changing anything")
	cmd.Flags().BoolVar(&watch, "watch", false, "keep running, syncing both ways as changes happen")
	cmd.Flags().BoolVar(&noAttachments, "no-attachments", false, "don't upload or download images (their links stay URLs in the file)")
	jsonFlag(cmd)
	cmd.AddCommand(newSyncStatus())
	return cmd
}

// syncError adds the action to take to errors a person can fix.
func syncError(err error) error {
	var cm *cloudsync.ConflictMarkersError
	if errors.As(err, &cm) {
		return err
	}
	if cloud.IsStatus(err, 409, "base_ahead") {
		return fmt.Errorf("%w (run 'todomd unlink' and 'todomd link' again)", err)
	}
	return err
}

func runWatch(ctx context.Context, s *cloudsync.Syncer, a *account) error {
	lock, pid, err := cloudsync.AcquireWatchLock(s.Path)
	if err != nil {
		return err
	}
	if lock == nil {
		if flagJSON {
			return printJSON(map[string]any{"file": s.Path, "watching": false, "watcher_pid": pid})
		}
		fmt.Printf("%s is already being synced by process %d\n", s.Path, pid)
		return nil
	}
	defer lock.Release()
	stamp := func() string { return time.Now().Format("15:04:05") }
	w := &cloudsync.Watcher{
		Syncer: s,
		OnRound: func(r *cloudsync.Result, err error) {
			if flagJSON {
				if err != nil {
					printNDJSON(map[string]any{"event": "error", "error": err.Error()})
					return
				}
				printNDJSON(struct {
					Event string `json:"event"`
					syncJSON
				}{"sync", toSyncJSON(s, r)})
				return
			}
			if err != nil {
				fmt.Printf("%s  error: %v\n", stamp(), err)
				return
			}
			fmt.Printf("%s  %s\n", stamp(), cloudsync.Describe(r))
			printConflicts(r)
		},
		OnState: func(state, detail string) {
			if flagJSON {
				printNDJSON(map[string]any{"event": "state", "state": state, "detail": detail})
				return
			}
			switch state {
			case cloudsync.StateOnline:
				fmt.Printf("%s  watching %s ⇅ %s (rev %d)\n", stamp(), filepath.Base(s.Path), s.Link.ProjectSlug, s.Link.BaseRev)
			default:
				fmt.Printf("%s  %s: %s\n", stamp(), state, detail)
			}
		},
	}
	err = w.Run(ctx)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func printNDJSON(v any) {
	enc := jsonEncoder()
	_ = enc.Encode(v)
}

func newSyncStatus() *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show this file's link and last sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := syncStore()
			if err != nil {
				return err
			}
			l, err := cloudsync.LoadLink(st.Path)
			if err != nil {
				return err
			}
			pid := cloudsync.WatcherPID(st.Path)
			head := -1
			if remote {
				a, err := openAccount(l.Host, true)
				if err != nil {
					return err
				}
				ctx, cancel := cmdContext()
				defer cancel()
				doc, err := a.client.Document(ctx, l.ProjectID, 0)
				if err != nil {
					return err
				}
				head = doc.Rev
			}
			// Where this project's images live on this machine, so tools such
			// as todomd-web can show the ones the file links to.
			images := ""
			if root, err := cloudsync.AttachmentRoot(l.Host); err == nil {
				images = filepath.Join(root, l.ProjectID)
			}
			if flagJSON {
				out := map[string]any{"file": st.Path, "link": l, "watcher_pid": pid, "attachments": images}
				if head >= 0 {
					out["head_rev"] = head
				}
				return printJSON(out)
			}
			fmt.Printf("file:     %s\nproject:  %s · %s\nhost:     %s\nbase rev: %d\n", st.Path, l.ProjectSlug, l.ProjectURL, cloud.HostName(l.Host), l.BaseRev)
			if head >= 0 {
				behind := ""
				if head > l.BaseRev {
					behind = fmt.Sprintf(" (%d behind)", head-l.BaseRev)
				}
				fmt.Printf("head rev: %d%s\n", head, behind)
			}
			if l.Jurisdiction != "" {
				fmt.Printf("region:   %s\n", l.Jurisdiction)
			}
			if l.Git != "" {
				fmt.Printf("git:      %s\n", l.Git)
			}
			if images != "" {
				fmt.Printf("images:   %s\n", images)
			}
			if l.FullFile != nil {
				fmt.Printf("mode:     full-file (%s, since %s)\n", l.FullFile.Reason, l.FullFile.Since.Local().Format(time.DateTime))
			}
			if l.ReadOnly {
				fmt.Printf("access:   read-only (local edits stay in the file, unpushed)\n")
			}
			if pid > 0 {
				fmt.Printf("watcher:  running (pid %d)\n", pid)
			} else {
				fmt.Printf("watcher:  none\n")
			}
			if ls := l.LastSync; ls != nil {
				state := "ok"
				if !ls.OK {
					state = "failed: " + ls.Error
				}
				fmt.Printf("last:     %s, %s\n", ls.At.Local().Format(time.DateTime), state)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "also ask the server for the head revision")
	jsonFlag(cmd)
	return cmd
}

func newOpen() *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the linked project in the browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := syncStore()
			if err != nil {
				return err
			}
			l, err := cloudsync.LoadLink(st.Path)
			if err != nil {
				return err
			}
			if !openBrowser(l.ProjectURL) {
				fmt.Println(l.ProjectURL)
			}
			return nil
		},
	}
}

// tuiSync is the TUI's background sync for a linked file, or nil when the
// file isn't linked or this machine isn't logged in. Another process already
// watching the file (a 'sync --watch') is left to it.
func tuiSync() tui.SyncFunc {
	s, _, err := openSyncer()
	if err != nil {
		return nil
	}
	return func(ctx context.Context, report func(string)) {
		lock, _, err := cloudsync.AcquireWatchLock(s.Path)
		if err != nil {
			return
		}
		if lock == nil {
			report("⇅ synced elsewhere")
			return
		}
		defer lock.Release()
		rev := func() string { return fmt.Sprintf("⇅ rev %d", s.Link.BaseRev) }
		w := &cloudsync.Watcher{
			Syncer: s,
			OnRound: func(r *cloudsync.Result, err error) {
				switch {
				case err != nil:
					report("⇅ error")
				case len(r.Conflicts) > 0:
					report(fmt.Sprintf("⇅ %d conflict%s", len(r.Conflicts), map[bool]string{true: "", false: "s"}[len(r.Conflicts) == 1]))
				default:
					report(rev())
				}
			},
			OnState: func(state, _ string) {
				switch state {
				case cloudsync.StateOnline:
					report(rev())
				case cloudsync.StateOffline:
					report("⇅ offline")
				case cloudsync.StatePaused:
					report("⇅ paused")
				}
			},
		}
		if err := w.Run(ctx); err != nil && ctx.Err() == nil {
			report("⇅ stopped")
		}
	}
}
