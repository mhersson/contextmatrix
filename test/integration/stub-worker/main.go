package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	var (
		systemPrompt string
		model        string
		allowedTools string
		resume       string
		print        bool
		verbose      bool
		inputFormat  string
		outputFormat string
	)
	flag.StringVar(&systemPrompt, "append-system-prompt", "", "")
	flag.StringVar(&model, "model", "", "")
	flag.StringVar(&allowedTools, "allowed-tools", "", "")
	flag.StringVar(&resume, "resume", "", "")
	flag.BoolVar(&print, "print", false, "")
	flag.BoolVar(&verbose, "verbose", false, "")
	flag.StringVar(&inputFormat, "input-format", "", "")
	flag.StringVar(&outputFormat, "output-format", "", "")
	flag.BoolVar(&print, "p", false, "")
	flag.Parse()

	_ = systemPrompt
	_ = allowedTools
	_ = resume
	_ = verbose
	_ = inputFormat
	_ = outputFormat
	_ = print

	cardID := os.Getenv("CM_CARD_ID")
	mcpURL := os.Getenv("CM_MCP_URL")
	mcpKey := os.Getenv("CM_MCP_API_KEY")
	interactive := os.Getenv("CM_INTERACTIVE") == "1"

	if cardID == "" || mcpURL == "" {
		fmt.Fprintln(os.Stderr, "stub-claude: CM_CARD_ID and CM_MCP_URL are required")
		os.Exit(2)
	}

	fmt.Fprintf(os.Stderr, "stub-claude: card=%s interactive=%v model=%s\n",
		cardID, interactive, model)

	if err := run(os.Stdout, os.Stdin, runArgs{
		cardID:      cardID,
		mcpURL:      mcpURL,
		mcpKey:      mcpKey,
		interactive: interactive,
		model:       model,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "stub-claude: %v\n", err)
		os.Exit(1)
	}
}

type runArgs struct {
	cardID      string
	mcpURL      string
	mcpKey      string
	interactive bool
	model       string
}

func run(stdout io.Writer, _ io.Reader, args runArgs) error {
	mcp := newMCP(args.mcpURL, args.mcpKey, "stub-"+args.cardID)
	project := os.Getenv("CM_PROJECT")
	if project == "" {
		return fmt.Errorf("CM_PROJECT not set")
	}

	body, err := mcp.GetCard(project, args.cardID)
	if err != nil {
		return fmt.Errorf("get_card: %w", err)
	}
	d := parseDirectives(body)

	if err := emitSystemInit(stdout, args.model, "stub-session-"+args.cardID); err != nil {
		return err
	}

	if args.interactive {
		return runHITL(stdout, os.Stdin, mcp, project, args, d)
	}
	return runAutonomous(stdout, mcp, project, args, d)
}

func runAutonomous(stdout io.Writer, mcp *mcpClient, project string, args runArgs, d directives) error {
	if err := emitToolUse(stdout, "t1", "claim_card", map[string]string{
		"project": project, "card_id": args.cardID,
	}); err != nil {
		return err
	}
	if err := mcp.ClaimCard(project, args.cardID); err != nil {
		return fmt.Errorf("claim_card: %w", err)
	}
	if err := emitToolResult(stdout, "t1", "claimed"); err != nil {
		return err
	}

	if d.hangAfterClaim {
		// Park until externally killed. We use time.Sleep instead of
		// `select {}` because once the MCP client's idle HTTP conns
		// time out (90s default) the runtime would otherwise see "all
		// goroutines asleep" and panic with deadlock — making the
		// whole scenario flaky on slow hosts. Sleep keeps the runtime's
		// timer goroutine alive indefinitely.
		time.Sleep(24 * time.Hour)
	}

	for i := 0; i < 3; i++ {
		if err := emitText(stdout, fmt.Sprintf("working… (step %d)", i+1)); err != nil {
			return err
		}
		if !d.skipHeartbeat {
			if err := mcp.Heartbeat(project, args.cardID); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := mcp.TransitionCard(project, args.cardID, "review"); err != nil {
		return fmt.Errorf("transition review: %w", err)
	}
	if err := mcp.TransitionCard(project, args.cardID, "done"); err != nil {
		return fmt.Errorf("transition done: %w", err)
	}
	if err := mcp.ReleaseCard(project, args.cardID); err != nil {
		return fmt.Errorf("release_card: %w", err)
	}
	return emitResult(stdout, "stub autonomous run complete")
}

func runHITL(stdout io.Writer, stdin io.Reader, mcp *mcpClient, project string, args runArgs, d directives) error {
	if err := emitToolUse(stdout, "t1", "claim_card", map[string]string{
		"project": project, "card_id": args.cardID,
	}); err != nil {
		return err
	}
	if err := mcp.ClaimCard(project, args.cardID); err != nil {
		return fmt.Errorf("claim_card: %w", err)
	}
	if err := emitToolResult(stdout, "t1", "claimed"); err != nil {
		return err
	}

	if err := emitText(stdout, "Awaiting input…"); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// The runner posts user messages via streammsg.BuildUserMessage,
		// which is shape: {type:"user",message:{role:"user",content:[{type:"text",text:"..."}]}}.
		// Extract the first text block from the nested content array.
		var frame struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		_ = json.Unmarshal([]byte(line), &frame)
		text := ""
		for _, block := range frame.Message.Content {
			if block.Type == "text" && block.Text != "" {
				text = block.Text
				break
			}
		}

		// Detect the runner's canned /promote stdin message and act per
		// directive.
		if isPromoteStdin(line) && d.promoteBehaviour == "respect" {
			if err := emitText(stdout, "Promote received; running autonomously."); err != nil {
				return err
			}
			return finishAutonomous(stdout, mcp, project, args)
		}

		if err := emitText(stdout, "you said: "+text); err != nil {
			return err
		}
		if !d.skipHeartbeat {
			if err := mcp.Heartbeat(project, args.cardID); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}

		if strings.Contains(strings.ToLower(text), "approve") {
			return finishAutonomous(stdout, mcp, project, args)
		}
	}

	// Stdin closed without approval — emit terminal frame, exit clean.
	return emitResult(stdout, "stub HITL exited without approval")
}

func finishAutonomous(stdout io.Writer, mcp *mcpClient, project string, args runArgs) error {
	if err := mcp.TransitionCard(project, args.cardID, "review"); err != nil {
		return fmt.Errorf("transition review: %w", err)
	}
	if err := mcp.TransitionCard(project, args.cardID, "done"); err != nil {
		return fmt.Errorf("transition done: %w", err)
	}
	if err := mcp.ReleaseCard(project, args.cardID); err != nil {
		return fmt.Errorf("release_card: %w", err)
	}
	return emitResult(stdout, "stub HITL run complete")
}

// isPromoteStdin recognises the canned message the runner writes to the
// container's stdin on /promote. The runner's webhook handler calls
// streammsg.BuildUserMessage with autonomousContent — see
// ../contextmatrix-runner/internal/webhook/handler.go.
// We match a stable substring of that content so a future tweak to wording
// doesn't break the stub.
func isPromoteStdin(line string) bool {
	return strings.Contains(line, "Autonomous mode has been enabled")
}
