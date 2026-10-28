package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/mmatczuk/jira-mcp/internal/jiramcp"
)

// Injected at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("version: %s\ncommit:  %s\ndate:    %s\ngo:      %s\n", version, commit, date, runtime.Version())
		return
	}

	cfg := jira.Config{
		URL:        requireEnv("JIRA_URL"),
		MaxRetries: 3,
		BaseDelay:  time.Second,
	}

	// Credential selection: Server/DC PAT (Bearer) > Cloud Basic > DC user/pass Basic.
	switch {
	case os.Getenv("JIRA_PERSONAL_TOKEN") != "":
		cfg.PersonalToken = os.Getenv("JIRA_PERSONAL_TOKEN")
		log.Printf("auth mode: Bearer Personal Access Token (Server/Data Center)")
	case os.Getenv("JIRA_EMAIL") != "" && os.Getenv("JIRA_API_TOKEN") != "":
		cfg.Email = os.Getenv("JIRA_EMAIL")
		cfg.APIToken = os.Getenv("JIRA_API_TOKEN")
		log.Printf("auth mode: Basic email + API token (Cloud)")
	case os.Getenv("JIRA_USER") != "" && os.Getenv("JIRA_PASS") != "":
		cfg.Email = os.Getenv("JIRA_USER")
		cfg.APIToken = os.Getenv("JIRA_PASS")
		log.Printf("auth mode: Basic user + password (Server/Data Center)")
	default:
		fmt.Fprintln(os.Stderr, "No Jira credentials found. Set one of:")
		fmt.Fprintln(os.Stderr, "  JIRA_EMAIL + JIRA_API_TOKEN   (Jira Cloud)")
		fmt.Fprintln(os.Stderr, "  JIRA_USER + JIRA_PASS         (Server/Data Center user + password)")
		fmt.Fprintln(os.Stderr, "  JIRA_PERSONAL_TOKEN           (Server/Data Center Personal Access Token)")
		os.Exit(1)
	}

	// JIRA_API_VERSION=2 switches the REST surface to the Server/Data Center
	// compatibility mode (legacy createmeta/search, wiki-markup strings).
	if v := os.Getenv("JIRA_API_VERSION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 2 && n != 3) {
			log.Fatalf("JIRA_API_VERSION must be 2 or 3, got %q", v)
		}
		cfg.APIVersion = n
		log.Printf("REST API version: %d", n)
	}

	client, err := jira.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create JIRA client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	myself, err := client.GetMyself(ctx)
	if err != nil {
		log.Fatalf("Failed to get current user: %v", err)
	}

	projects, err := client.GetAllProjects(ctx)
	if err != nil {
		log.Fatalf("Failed to list projects: %v", err)
	}

	s := jiramcp.NewServer(client, myself, projects)
	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "Required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return v
}
