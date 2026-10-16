package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/app"
	"github.com/RobertLMcCrary/D2L-MCP/internal/auth"
	"github.com/RobertLMcCrary/D2L-MCP/internal/canvas"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"github.com/RobertLMcCrary/D2L-MCP/internal/mcpserver"
)

type school struct {
	Name         string
	LMSHost      string
	SyllabusHost string
}

var schools = map[string]school{
	"ksu": {
		Name:         "Kennesaw State University",
		LMSHost:      "https://kennesaw.view.usg.edu",
		SyllabusHost: "https://kennesaw.simplesyllabus.com",
	},
	"kennesaw": {
		Name:         "Kennesaw State University",
		LMSHost:      "https://kennesaw.view.usg.edu",
		SyllabusHost: "https://kennesaw.simplesyllabus.com",
	},
	"gsu": {
		Name:    "Georgia State University",
		LMSHost: "https://gastate.view.usg.edu",
	},
	"gastate": {
		Name:    "Georgia State University",
		LMSHost: "https://gastate.view.usg.edu",
	},
}

func main() {
	command := "serve"

	args := os.Args[1:]
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}

	var err error

	switch command {
	case "serve":
		err = serve()
	case "setup":
		err = setup(args)
	case "login":
		err = login(args)
	case "doctor":
		err = doctor(args)
	case "version", "--version", "-v":
		fmt.Println(mcpserver.Version)
	case "help", "--help", "-h":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "d2l-mcp:", err)
		os.Exit(1)
	}
}

func serve() error {
	service, err := app.Load()
	if err != nil {
		return err
	}

	return mcpserver.Serve(service)
}

func setup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	host := flags.String("host", "", "LMS HTTPS host")
	provider := flags.String("provider", "", "LMS provider: d2l or canvas")
	toolProfile := flags.String("tool-profile", "", "Canvas tool catalog: full or compact (discover and invoke operations)")
	enableWrites := flags.Bool("enable-writes", false, "Enable academic writes (submissions, posts, messages)")
	syllabusHost := flags.String("syllabus-host", "", "SimpleSyllabus HTTPS host")
	schoolName := flags.String("school", "", "School preset: ksu or gsu")
	show := flags.Bool("show", false, "Show current configuration")
	list := flags.Bool("list-schools", false, "List school presets")
	allowPrivate := flags.Bool("allow-private-host", false, "Allow a private host for local testing")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *list {
		fmt.Println("ksu\tKennesaw State University")
		fmt.Println("gsu\tGeorgia State University")
		return nil
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	current, err := config.Load(paths)
	if err != nil {
		return err
	}

	if *show {
		return writeJSON(current)
	}
	if *provider != "" {
		current.Provider = *provider
	}
	if *toolProfile != "" {
		current.ToolProfile = *toolProfile
	}
	writesSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "enable-writes" {
			writesSet = true
			current.EnableWrites = *enableWrites
		}
	})

	if *schoolName != "" {
		if *provider == "canvas" {
			return fmt.Errorf("Brightspace school presets cannot be used with --provider canvas; supply --host")
		}
		preset, ok := schools[*schoolName]
		if !ok {
			return fmt.Errorf("unknown school preset %q; use --list-schools", *schoolName)
		}
		current.School, current.LMSHost, current.SyllabusHost = preset.Name, preset.LMSHost, preset.SyllabusHost
		current.Provider = "d2l"
		// Switching schools must not silently carry over write access.
		if !writesSet {
			current.EnableWrites = false
		}
	}

	if *host != "" {
		current.LMSHost = *host
	}

	if *syllabusHost != "" {
		current.SyllabusHost = *syllabusHost
	}

	if current.LMSHost == "" {
		return fmt.Errorf("--host or --school is required")
	}

	if err := config.Validate(&current, *allowPrivate); err != nil {
		return err
	}

	if err := config.Save(paths, current); err != nil {
		return err
	}

	fmt.Printf("Configured %s\n", current.LMSHost)
	return nil
}

func login(args []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	headless := flags.Bool("headless", false, "Run browser without a visible window")
	timeout := flags.Duration("timeout", 5*time.Minute, "Maximum login time")
	tokenStdin := flags.Bool("token-stdin", false, "Read a Canvas token from stdin; verify before storing")

	if err := flags.Parse(args); err != nil {
		return err
	}

	service, err := app.Load()
	if err != nil {
		return err
	}

	if service.Config.LMSHost == "" {
		return fmt.Errorf("no school configured; run d2l-mcp setup first")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if service.Config.Provider == "canvas" {
		if !*tokenStdin {
			return fmt.Errorf("Canvas uses access tokens: run login --token-stdin or set CANVAS_TOKEN")
		}
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 16<<10))
		if err != nil {
			return err
		}
		if len(raw) >= 16<<10 {
			return fmt.Errorf("token input exceeds limit")
		}
		client, err := canvas.New(service.Config.LMSHost, strings.TrimSpace(string(raw)), false, nil)
		if err != nil {
			return err
		}
		checkCtx, stop := context.WithTimeout(ctx, min(*timeout, 45*time.Second))
		defer stop()
		if _, err := client.Do(checkCtx, lms.Request{Path: "/api/v1/users/self/profile"}); err != nil {
			return err
		}
		if err := auth.SaveCanvas(service.Paths, service.Config.LMSHost, string(raw)); err != nil {
			return err
		}
		fmt.Println("Canvas token verified and stored for the configured host")
		return nil
	}

	options := auth.LoginOptions{
		Headless: *headless,
		Timeout:  *timeout,
	}

	status, err := auth.Login(
		ctx,
		service.Paths,
		service.Config.LMSHost,
		options,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Authenticated; token expires %s\n", status.ExpiresAt.Format(time.RFC3339))
	return nil
}

func doctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	network := flags.Bool("network", true, "Perform live API checks")

	if err := flags.Parse(args); err != nil {
		return err
	}

	service, err := app.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if service.Config.Provider == "canvas" {
		return writeJSON(service.ProviderStatus(ctx, *network))
	}
	return writeJSON(service.Status(ctx, *network))
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: d2l-mcp [serve|setup|login|doctor|version]

  serve    Start the local stdio MCP server (default)
  setup    Configure --provider d2l|canvas and --host HTTPS_ORIGIN
           --enable-writes for submissions and posts; Canvas: --tool-profile full|compact
  login    Brightspace browser login; Canvas: --token-stdin (or CANVAS_TOKEN)
  doctor   Print structured setup and API diagnostics
  version  Print the server version`)
}
