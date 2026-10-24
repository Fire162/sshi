package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/Fire162/sshi/pkg/client"
	"github.com/Fire162/sshi/pkg/config"
	"github.com/Fire162/sshi/pkg/sanitize"
	"github.com/Fire162/sshi/pkg/session"
	"github.com/Fire162/sshi/pkg/tui"
)

const version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\033[1;31mError:\033[0m %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(args) == 0 {
		// If last session exists, reconnect directly
		if cfg.LastSession != "" {
			fmt.Printf("Reconnecting to last session (%s)...\n", cfg.LastSession)
			return connectSession(cfg.LastSession, cfg, "", "")
		}
		printUsage()
		return nil
	}

	cmd := args[0]
	switch cmd {
	case "help", "-h", "--help":
		printUsage()
		return nil

	case "version", "-v", "--version":
		fmt.Printf("sshi version %s\n", version)
		return nil

	case "list", "ls":
		return runList(cfg)

	case "save":
		return runSave(args[1:], cfg)

	case "remove", "rm", "delete":
		return runRemove(args[1:], cfg)

	case "info":
		return runInfo(args[1:], cfg)

	case "pipe":
		return runPipe(args[1:], cfg)

	case "run":
		return runCommand(args[1:], cfg)

	case "connect":
		return handleConnectFlags(args[1:], cfg)

	default:
		// If starts with - (flag) or is a target/alias
		if strings.HasPrefix(cmd, "-") {
			return handleConnectFlags(args, cfg)
		}
		// Treat as target or alias
		return connectSession(cmd, cfg, "", "")
	}
}

func handleConnectFlags(args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	var connectArg string
	var serverArg string
	var nameArg string
	var writePassArg string

	fs.StringVar(&connectArg, "c", "", "Session target (URL or ID#key)")
	fs.StringVar(&connectArg, "connect", "", "Session target (URL or ID#key)")
	fs.StringVar(&serverArg, "s", "", "Custom sshx server URL")
	fs.StringVar(&serverArg, "server", "", "Custom sshx server URL")
	fs.StringVar(&nameArg, "n", "", "Participant display name")
	fs.StringVar(&nameArg, "name", "", "Participant display name")
	fs.StringVar(&writePassArg, "w", "", "Write authentication password")
	fs.StringVar(&writePassArg, "write-password", "", "Write authentication password")

	if err := fs.Parse(args); err != nil {
		return err
	}

	targetInput := connectArg
	if targetInput == "" && len(fs.Args()) > 0 {
		targetInput = fs.Args()[0]
	}

	return connectSession(targetInput, cfg, nameArg, serverArg)
}

func connectSession(rawInput string, cfg *config.Config, displayName string, serverURL string) error {
	resolved, err := cfg.ResolveTarget(rawInput)
	if err != nil {
		return err
	}

	target, err := session.ParseTarget(resolved, serverURL)
	if err != nil {
		return fmt.Errorf("invalid session target: %w", err)
	}

	if displayName == "" {
		if cfg.DefaultName != "" {
			displayName = cfg.DefaultName
		} else if u, err := user.Current(); err == nil && u.Username != "" {
			displayName = u.Username
		} else {
			displayName = "sshi-user"
		}
	}
	displayName = sanitize.CleanSingleLine(displayName, 32)

	fmt.Printf("\033[1;36mConnecting to sshx session: \033[0;37m%s\033[0m\n", target.String())

	var terminal *tui.Terminal

	cb := client.Callbacks{
		OnConnected: func(uid uint32, sessionName string) {
			// Ready to attach terminal
		},
		OnTerminalData: func(sid uint32, data []byte) {
			if terminal != nil {
				terminal.HandleTerminalData(sid, data)
			}
		},
		OnShellsUpdated: func(shells []client.ShellInfo) {
			if terminal != nil {
				terminal.HandleShellsUpdated(shells)
			}
		},
		OnChatMessage: func(chat client.MsgChat) {
			if terminal != nil {
				terminal.HandleChatMessage(chat)
			}
		},
		OnError: func(err error) {
			if terminal != nil {
				terminal.Close()
			}
			fmt.Fprintf(os.Stderr, "\r\n\033[1;31mSession Error: %v\033[0m\r\n", err)
		},
		OnDisconnect: func(err error) {
			if terminal != nil {
				terminal.Close()
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "\r\n\033[1;33mDisconnected: %v\033[0m\r\n", err)
			} else {
				fmt.Println("\r\n\033[1;32mSession closed.\033[0m")
			}
		},
	}

	cli := client.NewClient(target, cb)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := cli.Connect(ctx, displayName); err != nil {
		return err
	}
	defer cli.Close()

	terminal = tui.NewTerminal(cli)
	return terminal.Run()
}

func runSave(args []string, cfg *config.Config) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sshi save <alias> <url-or-token> [notes]")
	}
	alias := args[0]
	if !sanitize.ValidateAlias(alias) {
		return fmt.Errorf("invalid alias name %q: must contain only letters, numbers, '.', '-', or '_'", alias)
	}

	target := args[1]
	notes := ""
	if len(args) >= 3 {
		notes = strings.Join(args[2:], " ")
	}

	saved, err := cfg.SaveSession(alias, target, notes)
	if err != nil {
		return err
	}
	fmt.Printf("\033[1;32mSaved session bookmark:\033[0m %q -> %s\n", saved.Name, saved.Target)
	return nil
}

func runList(cfg *config.Config) error {
	sessions := cfg.ListSessions()
	if len(sessions) == 0 {
		fmt.Println("No saved sessions. Save one with: sshi save <alias> <target>")
		if cfg.LastSession != "" {
			fmt.Printf("Last connected session: %s\n", cfg.LastSession)
		}
		return nil
	}

	fmt.Println("\033[1;37mSaved Sessions:\033[0m")
	for _, s := range sessions {
		lastUsed := "never"
		if !s.LastUsedAt.IsZero() {
			lastUsed = s.LastUsedAt.Format("2006-01-02 15:04:05")
		}
		notes := ""
		if s.Notes != "" {
			notes = fmt.Sprintf(" - %s", s.Notes)
		}
		marker := " "
		if s.Target == cfg.LastSession {
			marker = "*"
		}
		fmt.Printf(" %s \033[1;36m%-16s\033[0m \033[38;5;244m[%s]\033[0m (last used: %s)%s\n",
			marker, s.Name, s.Target, lastUsed, notes)
	}
	fmt.Println("\nRun: sshi <alias> to connect directly.")
	return nil
}

func runRemove(args []string, cfg *config.Config) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sshi remove <alias>")
	}
	alias := args[0]
	if err := cfg.DeleteSession(alias); err != nil {
		return err
	}
	fmt.Printf("\033[1;32mRemoved session bookmark:\033[0m %q\n", alias)
	return nil
}

func runInfo(args []string, cfg *config.Config) error {
	var asJSON bool
	var cleanArgs []string
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
		} else {
			cleanArgs = append(cleanArgs, a)
		}
	}

	targetInput := ""
	if len(cleanArgs) > 0 {
		targetInput = cleanArgs[0]
	}
	resolved, err := cfg.ResolveTarget(targetInput)
	if err != nil {
		return err
	}

	target, err := session.ParseTarget(resolved, "")
	if err != nil {
		return err
	}

	readyChan := make(chan struct{})
	var cli *client.Client

	cb := client.Callbacks{
		OnConnected: func(uid uint32, sessionName string) {
			go func() {
				time.Sleep(1 * time.Second)
				close(readyChan)
			}()
		},
	}

	cli = client.NewClient(target, cb)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := cli.Connect(ctx, "sshi-info"); err != nil {
		return err
	}
	defer cli.Close()

	select {
	case <-readyChan:
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for session info")
	}

	shells := cli.GetShells()
	users := cli.GetUsers()
	rtt, shellLat := cli.Latencies()

	if asJSON {
		infoMap := map[string]interface{}{
			"id":           target.ID,
			"sessionName":  cli.SessionName(),
			"host":         target.Host,
			"shells":       shells,
			"users":        users,
			"rttLatency":   rtt,
			"shellLatency": shellLat,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(infoMap)
	}

	fmt.Printf("\033[1;36mSession Target:\033[0m   %s\n", target.String())
	fmt.Printf("\033[1;36mSession Name:\033[0m     %s\n", sanitize.CleanSingleLine(cli.SessionName(), 60))
	fmt.Printf("\033[1;36mActive Shells:\033[0m    %d\n", len(shells))
	for _, s := range shells {
		fmt.Printf("  • Shell #%d: %dx%d (pos: %d, %d)\n", s.ID, s.Size.Cols, s.Size.Rows, s.Size.X, s.Size.Y)
	}
	fmt.Printf("\033[1;36mConnected Users:\033[0m  %d\n", len(users))
	for _, u := range users {
		cleanU := sanitize.CleanSingleLine(u.User.Name, 30)
		fmt.Printf("  • User #%d: %s (canWrite: %v)\n", u.ID, cleanU, u.User.CanWrite)
	}
	if rtt > 0 {
		fmt.Printf("\033[1;36mLatency:\033[0m          RTT: %dms, Shell: %dms\n", rtt, shellLat)
	}
	return nil
}

func runPipe(args []string, cfg *config.Config) error {
	targetInput := ""
	if len(args) > 0 {
		targetInput = args[0]
	}

	resolved, err := cfg.ResolveTarget(targetInput)
	if err != nil {
		return err
	}

	target, err := session.ParseTarget(resolved, "")
	if err != nil {
		return err
	}

	var activeSID uint32
	readyChan := make(chan struct{})
	doneChan := make(chan struct{})

	cb := client.Callbacks{
		OnShellsUpdated: func(shells []client.ShellInfo) {
			if len(shells) > 0 && activeSID == 0 {
				activeSID = shells[0].ID
				select {
				case <-readyChan:
				default:
					close(readyChan)
				}
			}
		},
		OnTerminalData: func(sid uint32, data []byte) {
			if sid == activeSID {
				os.Stdout.Write(data)
			}
		},
		OnDisconnect: func(err error) {
			select {
			case <-doneChan:
			default:
				close(doneChan)
			}
		},
	}

	cli := client.NewClient(target, cb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cli.Connect(ctx, "agent-pipe"); err != nil {
		return err
	}
	defer cli.Close()

	// Wait for first shell
	select {
	case <-readyChan:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for shell in session")
	}

	// Trap SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Forward stdin to shell in background
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				select {
				case <-doneChan:
				default:
					close(doneChan)
				}
				return
			}
			if n > 0 {
				_ = cli.SendInput(activeSID, buf[:n])
			}
		}
	}()

	select {
	case <-doneChan:
	case <-sigChan:
	}
	return nil
}

func runCommand(args []string, cfg *config.Config) error {
	waitTimeout := 5 * time.Second
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		if args[i] == "--timeout" && i+1 < len(args) {
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				waitTimeout = d
			}
			i++
		} else if strings.HasPrefix(args[i], "--timeout=") {
			val := strings.TrimPrefix(args[i], "--timeout=")
			if d, err := time.ParseDuration(val); err == nil {
				waitTimeout = d
			}
		} else {
			cleanArgs = append(cleanArgs, args[i])
		}
	}

	if len(cleanArgs) < 2 {
		return fmt.Errorf("usage: sshi run [--timeout <dur>] <target-or-alias> \"<command>\"")
	}
	targetInput := cleanArgs[0]
	command := cleanArgs[1]
	if !strings.HasSuffix(command, "\n") {
		command += "\n"
	}

	resolved, err := cfg.ResolveTarget(targetInput)
	if err != nil {
		return err
	}
	target, err := session.ParseTarget(resolved, "")
	if err != nil {
		return err
	}

	var activeSID uint32
	readyChan := make(chan struct{})
	doneChan := make(chan struct{})

	cb := client.Callbacks{
		OnShellsUpdated: func(shells []client.ShellInfo) {
			if len(shells) > 0 && activeSID == 0 {
				activeSID = shells[0].ID
				select {
				case <-readyChan:
				default:
					close(readyChan)
				}
			}
		},
		OnTerminalData: func(sid uint32, data []byte) {
			if sid == activeSID {
				os.Stdout.Write(data)
			}
		},
		OnDisconnect: func(err error) {
			select {
			case <-doneChan:
			default:
				close(doneChan)
			}
		},
	}

	cli := client.NewClient(target, cb)
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout+5*time.Second)
	defer cancel()

	if err := cli.Connect(ctx, "agent-runner"); err != nil {
		return err
	}
	defer cli.Close()

	select {
	case <-readyChan:
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for active shell")
	}

	// Small pause to allow terminal init chunks to flush
	time.Sleep(300 * time.Millisecond)

	// Send command
	if err := cli.SendInput(activeSID, []byte(command)); err != nil {
		return err
	}

	// Read output for waitTimeout duration or until disconnect
	select {
	case <-time.After(waitTimeout):
	case <-doneChan:
	}

	return nil
}

func printUsage() {
	fmt.Printf(`SSHI - Terminal client for sshx.io

USAGE:
  sshi [command] [options] [target]

DIRECT CONNECTION:
  sshi <target>                     Connect directly to a session
  sshi                              Reconnect to the last used session
  sshi --connect <target>           Connect using explicit flag

  <target> can be:
    • Full URL:     https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd
    • Token:        hr2hQ8YKuf#lFRwv8c7cZhJRd
    • Saved Alias:  prod, dev, my-server

SESSION BOOKMARKING:
  sshi save <alias> <target> [notes]  Bookmark a session with a memorable alias
  sshi list                           List all saved sessions
  sshi remove <alias>                 Remove a saved bookmark

SCRIPTING & AUTOMATION:
  sshi run [--timeout 10s] <target> "<command>"  Execute command remotely and stream output
  sshi pipe <target>                             Raw bidirectional stdin/stdout stream for pipes
  sshi info <target> [--json]                    Inspect active shells, participants & latency

INTERACTIVE SHORTCUTS (Prefix: Ctrl+A):
  Ctrl+A c       Create a new shell / tab
  Ctrl+A n / p   Next / Previous tab
  Ctrl+A 1..9    Switch directly to Tab 1-9
  Ctrl+A x       Close current shell
  Ctrl+A t       Open Chat bar to send a message to room
  Ctrl+A m       View recent Chat Message History modal
  Ctrl+A u       List connected users and permissions
  Ctrl+A N       Change your participant nickname on the fly
  Ctrl+A b       Toggle bottom status bar
  Ctrl+A Ctrl+A  Send literal Ctrl+A
  Ctrl+A d / q   Detach / exit cleanly

OPTIONS:
  -c, --connect <target>     Session target (URL, token, or alias)
  -s, --server <url>         Custom sshx relay server (default: https://sshx.io)
  -n, --name <name>          Participant display name (default: current user)
  -w, --write-password <pw>  Write authentication password (if read-only)
  -v, --version              Print version
  -h, --help                 Show this help screen
`)
}
