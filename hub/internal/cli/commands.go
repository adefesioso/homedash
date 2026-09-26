package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
)

// command is one subcommand: the arguments it takes, one line on what it
// does, and the API call it becomes.
type command struct {
	name, args, help string
	run              func(ctx context.Context, c *Client, args []string, jsonOut bool) error
}

// Run is main's dispatcher for everything that is not the hub's own
// subcommand. --json anywhere on the line prints the hub's answer as it
// came; otherwise a short table, or the text itself.
func Run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	jsonOut := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--json" || a == "-json" {
			jsonOut = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		usage()
		return nil
	}
	name, rest := rest[0], rest[1:]
	switch name {
	case "login":
		if len(rest) != 1 {
			return errors.New("usage: homedash login <hub-url>")
		}
		return login(ctx, rest[0])
	case "logout":
		c, err := Load()
		if err == nil {
			_, _ = newClient(c).Do(ctx, http.MethodPost, "/auth/logout", nil)
		}
		return Forget()
	case "whoami":
		c, err := Load()
		if err != nil {
			return err
		}
		if c.Role == "" {
			fmt.Printf("%s with an API token\n", c.Hub)
		} else {
			fmt.Printf("%s as %s (%s)\n", c.Hub, c.Name, c.Role)
		}
		return nil
	}
	cmd, ok := commands[name]
	if !ok {
		usage()
		return fmt.Errorf("unknown command %q", name)
	}
	cfg, err := Load()
	if err != nil {
		return err
	}
	return cmd.run(ctx, newClient(cfg), rest, jsonOut)
}

func usage() {
	fmt.Fprint(os.Stderr, "usage: homedash <command> [args] [--json]\n\n  login HUB-URL                    sign in through your browser\n  logout                           end this session\n  whoami                           which hub, as whom\n")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(os.Stderr, 2, 2, 2, ' ', 0)
	for _, n := range names {
		c := commands[n]
		fmt.Fprintf(tw, "  %s %s\t%s\n", n, c.args, c.help)
	}
	tw.Flush()
	fmt.Fprintln(os.Stderr, "\nOn the hub itself: homedash serve | recover | invite | token NAME | version")
}

var commands = map[string]command{}

func add(c command) { commands[c.name] = c }

// out prints raw JSON as it came, or hands a decoded value to a human
// printer.
func out(raw []byte, jsonOut bool, human func(v any)) error {
	if jsonOut || human == nil {
		os.Stdout.Write(raw)
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			fmt.Println()
		}
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		os.Stdout.Write(raw)
		return nil
	}
	human(v)
	return nil
}

// table prints rows with a header, tab-aligned.
func table(header []string, rows [][]string) {
	tw := tabwriter.NewWriter(os.Stdout, 2, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', 2, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func list(v any, key string) []map[string]any {
	if key != "" {
		if m, ok := v.(map[string]any); ok {
			v = m[key]
		}
	}
	arr, _ := v.([]any)
	rows := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			rows = append(rows, m)
		}
	}
	return rows
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 72 {
		s = s[:69] + "…"
	}
	return s
}

// readText is a file, or stdin for "-" or when no file was named and
// stdin is not a terminal.
func readText(path string) (string, error) {
	if path == "" || path == "-" {
		if fi, _ := os.Stdin.Stat(); path == "" && fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("give a file with -f, or pipe the text in")
		}
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parse takes flags from anywhere on the line and returns the
// positionals in order; everything after "--" is positional verbatim,
// which is how a command with its own flags reaches `run`.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for len(args) > 0 {
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		if !strings.HasPrefix(args[0], "-") || args[0] == "-" {
			pos, args = append(pos, args[0]), args[1:]
			continue
		}
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil
		}
		args = rest
	}
	return pos, nil
}

func need(args []string, n int, usage string) error {
	if len(args) < n {
		return errors.New("usage: homedash " + usage)
	}
	return nil
}

func init() {
	add(command{"hosts", "", "every enrolled machine and its status", func(ctx context.Context, c *Client, _ []string, j bool) error {
		raw, err := c.get(ctx, "/hosts", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, h := range list(v, "") {
				rows = append(rows, []string{str(h, "name"), str(h, "status"), str(h, "addr"), str(h, "agentModel"), str(h, "lastSeen")})
			}
			table([]string{"HOST", "STATUS", "ADDR", "AGENT MODEL", "LAST SEEN"}, rows)
		})
	}})
	add(command{"devices", "", "every device the remotes can see", func(ctx context.Context, c *Client, _ []string, j bool) error {
		raw, err := c.get(ctx, "/devices", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, d := range list(v, "devices") {
				rows = append(rows, []string{str(d, "id"), str(d, "kind") + ":" + str(d, "addr"), str(d, "name"), str(d, "guessName"), str(d, "guessKind"), str(d, "lastSeen")})
			}
			table([]string{"ID", "DEVICE", "NAME", "GUESS", "KIND", "LAST SEEN"}, rows)
		})
	}})
	add(command{"storage", "", "clusters, members, health; workspaces", func(ctx context.Context, c *Client, _ []string, j bool) error {
		cl, err := c.get(ctx, "/clusters", nil)
		if err != nil {
			return err
		}
		ws, err := c.get(ctx, "/workspaces", nil)
		if err != nil {
			return err
		}
		raw := []byte(`{"clusters":` + strings.TrimSpace(string(cl)) + `,"workspaces":` + strings.TrimSpace(string(ws)) + "}\n")
		return out(raw, j, nil)
	}})
	add(command{"apps", "[HOST]", "stacks live from every remote", func(ctx context.Context, c *Client, args []string, j bool) error {
		raw, err := c.get(ctx, "/apps", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, s := range list(v, "stacks") {
				if len(args) > 0 && str(s, "host") != args[0] {
					continue
				}
				rows = append(rows, []string{str(s, "host"), str(s, "name"), str(s, "status"), strconv.Itoa(len(list(s["containers"], "")))})
			}
			table([]string{"HOST", "STACK", "STATUS", "CONTAINERS"}, rows)
			if m, ok := v.(map[string]any); ok {
				if errs, ok := m["errors"].([]any); ok {
					for _, e := range errs {
						fmt.Fprintln(os.Stderr, "!", e)
					}
				}
			}
		})
	}})
	add(command{"logs", "HOST STACK [-n LINES]", "a stack's recent logs", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("logs")
		n := fs.Int("n", 200, "lines")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "logs HOST STACK [-n LINES]"); err != nil {
			return err
		}
		raw, err := c.get(ctx, "/hosts/"+esc(pos[0])+"/apps/"+esc(pos[1])+"/logs", url.Values{"lines": {strconv.Itoa(*n)}})
		if err != nil {
			return err
		}
		return out(raw, true, nil)
	}})
	add(command{"catalog", "", "the app catalog", func(ctx context.Context, c *Client, _ []string, j bool) error {
		raw, err := c.get(ctx, "/apps/catalog", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, e := range list(v, "") {
				rows = append(rows, []string{str(e, "name"), firstLine(str(e, "description"))})
			}
			table([]string{"NAME", "DESCRIPTION"}, rows)
		})
	}})
	add(command{"place", "NEEDS-JSON", "rank the fleet for a stack's needs, e.g. '{\"memoryMB\":8192,\"gpu\":true}'", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 1, "place NEEDS-JSON"); err != nil {
			return err
		}
		var needs map[string]any
		if err := json.Unmarshal([]byte(args[0]), &needs); err != nil {
			return fmt.Errorf("needs must be JSON: %w", err)
		}
		raw, err := c.Do(ctx, http.MethodPost, "/apps/placement", needs)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, e := range list(v, "") {
				rows = append(rows, []string{str(e, "host"), str(e, "fits"), str(e, "score"), str(e, "why")})
			}
			table([]string{"HOST", "FITS", "SCORE", "WHY"}, rows)
		})
	}})
	add(command{"fit", "HOST [-n N]", "what llmfit says fits on a machine", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("fit")
		n := fs.Int("n", 8, "how many")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "fit HOST [-n N]"); err != nil {
			return err
		}
		raw, err := c.get(ctx, "/hosts/"+esc(pos[0])+"/fit", url.Values{"n": {strconv.Itoa(*n)}})
		if err != nil {
			return err
		}
		return out(raw, true, nil)
	}})
	add(command{"secrets", "", "the secret names a host may use, never a value", func(ctx context.Context, c *Client, _ []string, j bool) error {
		raw, err := c.get(ctx, "/secrets", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, s := range list(v, "") {
				hosts := str(s, "hosts")
				if hosts == "[]" || hosts == "" {
					hosts = "every host"
				}
				rows = append(rows, []string{str(s, "name"), hosts, str(s, "updated")})
			}
			table([]string{"NAME", "HOSTS", "UPDATED"}, rows)
		})
	}})
	add(command{"events", "[--before EVENT-ID] [--limit N]", "the event log, newest first", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("events")
		before := fs.Int64("before", 0, "only events older than this id")
		limit := fs.Int("limit", 200, "how many to return, at most 500")
		if _, err := parse(fs, args); err != nil {
			return err
		}
		q := url.Values{}
		if *before > 0 {
			q.Set("before", strconv.FormatInt(*before, 10))
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		raw, err := c.get(ctx, "/events", q)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			var rows [][]string
			for _, e := range list(v, "") {
				rows = append(rows, []string{str(e, "at"), str(e, "kind"), str(e, "subject"), firstLine(str(e, "message"))})
			}
			table([]string{"AT", "KIND", "SUBJECT", "MESSAGE"}, rows)
		})
	}})
	add(command{"jobs", "[HOST]", "recent jobs, newest first", func(ctx context.Context, c *Client, args []string, j bool) error {
		q := url.Values{}
		if len(args) > 0 {
			q.Set("host", args[0])
		}
		raw, err := c.get(ctx, "/jobs", q)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) { jobTable(list(v, "")) })
	}})
	add(command{"job", "ID [--after EVENT-ID]", "one job: state, report, newest events", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("job")
		after := fs.Int64("after", 0, "events after this id")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "job ID [--after EVENT-ID]"); err != nil {
			return err
		}
		id := pos[0]
		jb, err := c.get(ctx, "/jobs/"+esc(id), nil)
		if err != nil {
			return err
		}
		ev, err := c.get(ctx, "/jobs/"+esc(id)+"/events", url.Values{"after": {strconv.FormatInt(*after, 10)}})
		if err != nil {
			return err
		}
		raw := []byte(`{"job":` + strings.TrimSpace(string(jb)) + `,"events":` + strings.TrimSpace(string(ev)) + "}\n")
		return out(raw, j, func(v any) {
			m, _ := v.(map[string]any)
			job, _ := m["job"].(map[string]any)
			jobTable([]map[string]any{job})
			if r := str(job, "report"); r != "" {
				fmt.Println("\n" + r)
			}
			if r := str(job, "reason"); r != "" {
				fmt.Println("\nreason:", r)
			}
			evs := list(m["events"], "")
			if len(evs) > 0 {
				fmt.Println()
				for _, e := range evs {
					fmt.Printf("%s  %s\n", str(e, "id"), firstLine(str(e, "line")))
				}
			}
		})
	}})
	add(command{"run", "HOST [-t SECONDS] [--forwards] -- CMD…", "one gated command on one host", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("run")
		t := fs.Int("t", 120, "timeout")
		fw := fs.Bool("forwards", false, "the vault and the hub's door reachable on the remote, as in a job")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "run HOST [-t SECONDS] [--forwards] -- CMD…"); err != nil {
			return err
		}
		cmd := strings.Join(pos[1:], " ")
		raw, err := c.Do(ctx, http.MethodPost, "/hosts/"+esc(pos[0])+"/run", map[string]any{"command": cmd, "timeoutSeconds": *t, "forwards": *fw})
		if err != nil {
			return err
		}
		if j {
			return out(raw, true, nil)
		}
		var r struct {
			ExitCode       int
			Stdout, Stderr string
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return out(raw, true, nil)
		}
		os.Stdout.WriteString(r.Stdout)
		os.Stderr.WriteString(r.Stderr)
		if r.ExitCode != 0 {
			os.Exit(r.ExitCode)
		}
		return nil
	}})
	add(command{"write", "HOST PATH [-f FILE] [--mode 0644] [--sudo]", "a file on a host, from -f or stdin", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("write")
		f := fs.String("f", "", "file to send; stdin otherwise")
		mode := fs.String("mode", "0644", "octal mode")
		sudo := fs.Bool("sudo", false, "write as root")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "write HOST PATH [-f FILE] [--mode 0644] [--sudo]"); err != nil {
			return err
		}
		content, err := readText(*f)
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, http.MethodPut, "/hosts/"+esc(pos[0])+"/file", map[string]any{"path": pos[1], "content": content, "mode": *mode, "sudo": *sudo})
		return err
	}})
	add(command{"lock", "HOST on|off", "the SSH lock: on, only the hub's account may log in", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 2, "lock HOST on|off"); err != nil {
			return err
		}
		var locked bool
		switch args[1] {
		case "on":
			locked = true
		case "off":
		default:
			return errors.New("usage: homedash lock HOST on|off")
		}
		_, err := c.Do(ctx, http.MethodPost, "/hosts/"+esc(args[0])+"/lock", map[string]any{"locked": locked})
		return err
	}})
	add(command{"name", "DEVICE NAME [KIND]", "your name and kind on a device, beside the guess; empty clears", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 2, "name DEVICE NAME [KIND]"); err != nil {
			return err
		}
		kind := ""
		if len(args) > 2 {
			kind = args[2]
		}
		raw, err := c.Do(ctx, http.MethodPut, "/devices/"+esc(args[0]), map[string]any{"name": args[1], "kind": kind})
		if err != nil {
			return err
		}
		return out(raw, true, nil)
	}})
	add(command{"deploy", "HOST NAME -f COMPOSE [-e ENV-FILE]", "install or update a stack", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("deploy")
		f := fs.String("f", "", "compose file; stdin otherwise")
		e := fs.String("e", "", "an optional .env file")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "deploy HOST NAME -f COMPOSE [-e ENV-FILE]"); err != nil {
			return err
		}
		compose, err := readText(*f)
		if err != nil {
			return err
		}
		env := ""
		if *e != "" {
			if env, err = readText(*e); err != nil {
				return err
			}
		}
		raw, err := c.Do(ctx, http.MethodPost, "/hosts/"+esc(pos[0])+"/apps", map[string]any{"name": pos[1], "compose": compose, "env": env})
		if err != nil {
			return err
		}
		return outputField(raw, j)
	}})
	add(command{"app", "HOST NAME start|stop|restart|pull|remove [--volumes]", "act on a stack", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("app")
		vol := fs.Bool("volumes", false, "with remove: delete the stack's volumes too")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 3, "app HOST NAME start|stop|restart|pull|remove [--volumes]"); err != nil {
			return err
		}
		raw, err := c.Do(ctx, http.MethodPost, "/hosts/"+esc(pos[0])+"/apps/"+esc(pos[1]), map[string]any{"action": pos[2], "volumes": *vol})
		if err != nil {
			return err
		}
		return outputField(raw, j)
	}})
	add(command{"start", "HOST [--cwd DIR] TEXT…", "give a remote's agent a job, written as an outcome", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("start")
		cwd := fs.String("cwd", "", "working directory on the host")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "start HOST [--cwd DIR] TEXT…"); err != nil {
			return err
		}
		host := pos[0]
		text := strings.Join(pos[1:], " ")
		if strings.TrimSpace(text) == "" {
			if text, _ = readText(""); strings.TrimSpace(text) == "" {
				return errors.New("usage: homedash start HOST [--cwd DIR] TEXT…  (or pipe the text in)")
			}
		}
		raw, err := c.Do(ctx, http.MethodPost, "/jobs", map[string]any{"host": host, "cwd": *cwd, "text": text})
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			m, _ := v.(map[string]any)
			fmt.Printf("job %s started on %s; follow it with: homedash job %s\n", str(m, "id"), str(m, "host"), str(m, "id"))
		})
	}})
	add(command{"correct", "ID TEXT…", "a follow-up round on a job", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 2, "correct ID TEXT…"); err != nil {
			return err
		}
		raw, err := c.Do(ctx, http.MethodPost, "/jobs/"+esc(args[0])+"/correct", map[string]any{"text": strings.Join(args[1:], " ")})
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			m, _ := v.(map[string]any)
			fmt.Printf("job %s: round %s, %s\n", str(m, "id"), str(m, "rounds"), str(m, "state"))
		})
	}})
	add(command{"rollback", "ID", "put a host back to the snapshot taken before a job", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 1, "rollback ID"); err != nil {
			return err
		}
		raw, err := c.Do(ctx, http.MethodPost, "/jobs/"+esc(args[0])+"/rollback", nil)
		if err != nil {
			return err
		}
		return out(raw, j, func(v any) {
			m, _ := v.(map[string]any)
			fmt.Printf("job %s on %s: %s\n", str(m, "id"), str(m, "host"), str(m, "snapshot"))
		})
	}})
	add(command{"reprovision", "HOST", "run the enrollment layout again on a host: accounts, key, hook, agent", func(ctx context.Context, c *Client, args []string, j bool) error {
		if err := need(args, 1, "reprovision HOST"); err != nil {
			return err
		}
		if _, err := c.Do(ctx, http.MethodPost, "/hosts/"+esc(args[0])+"/reprovision", nil); err != nil {
			return err
		}
		fmt.Println("re-provisioning in the background; it ends as an event")
		return nil
	}})
	add(command{"rebuild", "HOST [-f SCRIPT]", "read a host's rebuild script, or replace it from -f or stdin", func(ctx context.Context, c *Client, args []string, j bool) error {
		fs := flags("rebuild")
		f := fs.String("f", "", "the whole new script")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "rebuild HOST [-f SCRIPT]"); err != nil {
			return err
		}
		host := pos[0]
		fi, _ := os.Stdin.Stat()
		piped := fi != nil && fi.Mode()&os.ModeCharDevice == 0
		if *f == "" && !piped {
			raw, err := c.get(ctx, "/hosts/"+esc(host), nil)
			if err != nil {
				return err
			}
			return out(raw, j, func(v any) {
				m, _ := v.(map[string]any)
				os.Stdout.WriteString(str(m, "rebuildScript"))
			})
		}
		script, err := readText(*f)
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, http.MethodPut, "/hosts/"+esc(host)+"/rebuild-script", script)
		return err
	}})
}

func jobTable(jobs []map[string]any) {
	var rows [][]string
	for _, jb := range jobs {
		rows = append(rows, []string{str(jb, "id"), str(jb, "host"), str(jb, "state"), str(jb, "rounds"), str(jb, "started"), firstLine(str(jb, "text"))})
	}
	table([]string{"ID", "HOST", "STATE", "ROUNDS", "STARTED", "TEXT"}, rows)
}

// outputField prints the "output" the deploy and action handlers return.
func outputField(raw []byte, jsonOut bool) error {
	return out(raw, jsonOut, func(v any) {
		m, _ := v.(map[string]any)
		os.Stdout.WriteString(str(m, "output"))
	})
}
