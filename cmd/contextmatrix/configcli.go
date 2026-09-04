package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mhersson/contextmatrix/internal/config"
	"gopkg.in/yaml.v3"
)

const configUsage = `usage: contextmatrix config <subcommand>

  defaults          print the complete default configuration as YAML
  validate <file>   load <file> exactly as the server would; exit 1 on the first error`

func runConfigCLI(args []string) int {
	return configCLI(args, os.Stdout, os.Stderr)
}

// configCLI dispatches a config subcommand. YAML and "ok" go to stdout, every
// error to stderr, so `contextmatrix config defaults > file` is safe.
func configCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, configUsage)

		return 2
	}

	switch args[0] {
	case "defaults":
		return runConfigDefaults(args[1:], stdout, stderr)
	case "validate":
		return runConfigValidate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "config: unknown subcommand %q\n\n%s\n", args[0], configUsage)

		return 2
	}
}

func runConfigDefaults(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: contextmatrix config defaults")

		return 2
	}

	out, err := yaml.Marshal(config.Defaults())
	if err != nil {
		fmt.Fprintf(stderr, "config defaults: %v\n", err)

		return 1
	}

	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintf(stderr, "config defaults: %v\n", err)

		return 1
	}

	return 0
}

func runConfigValidate(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: contextmatrix config validate <file>")

		return 2
	}

	path := args[0]

	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(stderr, "config validate: %v\n", err)

		return 1
	}

	if _, err := config.Load(path); err != nil {
		fmt.Fprintf(stderr, "config validate: %v\n", err)

		return 1
	}

	fmt.Fprintf(stdout, "%s: ok\n", path)

	return 0
}
