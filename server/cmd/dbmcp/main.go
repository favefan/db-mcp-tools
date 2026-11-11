package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"dbmcp.local/server/internal/api"
	"dbmcp.local/server/internal/diagnostics"
	"dbmcp.local/server/internal/model"
	run "dbmcp.local/server/internal/runtime"
	"dbmcp.local/server/internal/storage"
	"github.com/spf13/cobra"
)

var resultWriter io.Writer = os.Stdout
var resultOutput *os.File
var errElevatedDone = errors.New("authorized operation finished")
var processLog *diagnostics.Logger
var processContext = context.Background()

func output(v any) error {
	if v == nil {
		return nil
	}
	e := json.NewEncoder(resultWriter)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func main() {
	os.Exit(execute())
}
func execute() (code int) {
	defer func() {
		if value := recover(); value != nil {
			diagnostics.Panic(processContext, value)
			fmt.Fprintln(os.Stderr, "dbmcp encountered an internal error; export diagnostics")
			code = 1
		}
		if processLog != nil {
			_ = processLog.Close()
			processLog = nil
		}
	}()
	if handled, code, err := run.ForwardInstalled(); handled || err != nil {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
		return code
	}
	defer func() {
		if resultOutput != nil {
			_ = resultOutput.Sync()
			_ = resultOutput.Close()
		}
	}()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := command().ExecuteContext(ctx); err != nil {
		if errors.Is(err, errElevatedDone) {
			return 0
		}
		diagnostics.Event(processContext, slog.LevelError, "command.failed", "error", diagnostics.ErrorInfo(err))
		if resultOutput != nil {
			_ = output(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	diagnostics.Event(processContext, slog.LevelInfo, "command.complete")
	return 0
}
func command() *cobra.Command {
	var dir, endpoint, input, method, username string
	var system, jsonFlag bool
	var elevate bool
	var resultFile string
	var managerSID string
	root := &cobra.Command{Use: "dbmcp", Short: "Database MCP Server, management CLI and STDIO Bridge", Version: model.Version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&dir, "data-dir", run.DefaultDir(), "MCP Server data directory")
	root.PersistentFlags().StringVar(&endpoint, "admin-url", "", "Admin endpoint (default: local deployment.toml)")
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Emit JSON (all business commands already return JSON)")
	root.PersistentFlags().BoolVar(&elevate, "elevate", false, "Ask Windows to authorize a native service or runtime operation")
	root.PersistentFlags().StringVar(&resultFile, "result-file", "", "Internal authorization result file")
	_ = root.PersistentFlags().MarkHidden("result-file")
	root.PersistentFlags().StringVar(&managerSID, "manager-sid", "", "Internal management user identity")
	_ = root.PersistentFlags().MarkHidden("manager-sid")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() != "version" {
			component, logDir := "cli", diagnostics.UserDir()
			if cmd.Name() == "bridge" {
				component = "bridge"
			}
			if cmd.Name() == "serve" || cmd.Name() == "service" && len(args) == 1 && args[0] == "run" {
				component = "server"
				logData := dir
				if cmd.Name() == "service" && !root.PersistentFlags().Changed("data-dir") && (runtime.GOOS == "windows" || system) {
					logData = run.SystemDir()
				}
				logDir = filepath.Join(logData, "logs")
			}
			var logErr error
			processLog, logErr = diagnostics.Open(logDir, component)
			if logErr != nil {
				fmt.Fprintln(os.Stderr, "Diagnostic file logging unavailable:", logErr)
			} else {
				slog.SetDefault(processLog.Logger)
			}
			trace := os.Getenv("DBMCP_TRACE_ID")
			if len(trace) != 32 || strings.Trim(trace, "0123456789abcdef") != "" {
				trace = model.ID()
			}
			cmd.SetContext(diagnostics.WithTrace(cmd.Context(), trace))
			processContext = cmd.Context()
			diagnostics.Event(cmd.Context(), slog.LevelInfo, "command.start", "command", cmd.CommandPath(), "build", diagnostics.Build(), "log_dir", logDir)
		}
		if resultFile != "" {
			if elevate {
				return errors.New("nested authorization is not supported")
			}
			file, err := run.OpenElevationResult(resultFile)
			if err != nil {
				return err
			}
			resultOutput = file
			resultWriter = file
		}
		if managerSID != "" {
			if resultFile == "" {
				return errors.New("management identity is only accepted by an authorized worker")
			}
			if err := storage.ConfigureManagerSID(managerSID); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(dir, "state.sqlite")); err == nil {
				if err = storage.GrantConfiguredManager(dir); err != nil {
					return err
				}
			}
		}
		if !elevate {
			return nil
		}
		allowed := cmd.Name() == "service" && len(args) == 1 && slices.Contains([]string{"install", "uninstall", "start", "stop", "restart"}, args[0])
		allowed = allowed || cmd.Parent() != nil && cmd.Parent().Name() == "runtime"
		if !allowed {
			return errors.New("authorization is only available for explicit service and runtime operations")
		}
		forward := []string{}
		for _, arg := range os.Args[1:] {
			if arg != "--elevate" && !strings.HasPrefix(arg, "--elevate=") {
				forward = append(forward, arg)
			}
		}
		value, err := run.Elevate(cmd.Context(), forward)
		if err != nil {
			return err
		}
		if err = output(value); err != nil {
			return err
		}
		return errElevatedDone
	}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show build, API and driver capabilities", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return output(run.VersionInfo()) }})
	var diagnosticOutput string
	diagnosticCommand := &cobra.Command{Use: "diagnostics", Short: "Export operational logs and a safe runtime snapshot (also works offline)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return exportDiagnostics(cmd.Context(), dir, endpoint, diagnosticOutput)
	}}
	diagnosticCommand.Flags().StringVar(&diagnosticOutput, "out", "", "Destination diagnostic JSON file")
	_ = diagnosticCommand.MarkFlagRequired("out")
	root.AddCommand(diagnosticCommand)
	runtimeCmd := &cobra.Command{Use: "runtime", Short: "Deploy MCP Server in a stable versioned runtime directory"}
	runtimeCmd.AddCommand(&cobra.Command{Use: "info", Short: "Read local deployment addresses without opening state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.Info(cmd.Context(), dir)
		if err != nil {
			return err
		}
		return output(v)
	}})
	runtimeCmd.AddCommand(&cobra.Command{Use: "prepare", Short: "Prepare the first managed runtime without a backup", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := run.Initialize(dir); err != nil {
			return err
		}
		value, err := run.Prepare(cmd.Context(), dir)
		if err != nil {
			return err
		}
		return output(value)
	}})
	var launch, asService bool
	var destination string
	runtimeCmd.PersistentFlags().BoolVar(&launch, "start", false, "Start after deployment (upgrade also resumes a previously running MCP Server)")
	runtimeCmd.PersistentFlags().BoolVar(&asService, "service", false, "Install or migrate as a native OS service")
	runtimeCmd.PersistentFlags().BoolVar(&system, "system", false, "Use a system service instead of a POSIX user service")
	runtimeCmd.PersistentFlags().StringVar(&username, "user", "", "Dedicated service account")
	runtimeCmd.AddCommand(&cobra.Command{Use: "deploy", Short: "Deploy this build while MCP Server is stopped; create a recovery snapshot", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := run.Initialize(dir); err != nil {
			return err
		}
		v, err := run.Deploy(cmd.Context(), dir)
		if err != nil {
			return err
		}
		return output(v)
	}})
	runtimeCmd.AddCommand(&cobra.Command{Use: "upgrade", Short: "Stop a managed MCP Server, back up, select this build, and resume its previous mode", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.Upgrade(cmd.Context(), dir, username, system, launch, asService)
		if err != nil {
			return err
		}
		return output(v)
	}})
	migrate := &cobra.Command{Use: "migrate", Short: "Move stopped state to an empty destination; preserve the source and its backup", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.Migrate(cmd.Context(), dir, destination, username, system, asService, launch)
		if err != nil {
			return err
		}
		return output(v)
	}}
	migrate.Flags().StringVar(&destination, "to", run.SystemDir(), "Empty destination data directory")
	runtimeCmd.AddCommand(migrate)
	runtimeCmd.AddCommand(&cobra.Command{Use: "rollback-program", Short: "Select the previous executable while stopped; state recovery remains explicit", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.RollbackProgram(dir)
		if err != nil {
			return err
		}
		return output(v)
	}})
	root.AddCommand(runtimeCmd)
	var allocatePorts bool
	initCmd := &cobra.Command{Use: "init", Short: "Initialize a private data directory and administrator credential", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.InitializePorts(dir, allocatePorts)
		if err != nil {
			return err
		}
		return output(v)
	}}
	initCmd.Flags().BoolVar(&allocatePorts, "allocate-ports", false, "Choose available loopback ports for a new deployment")
	root.AddCommand(initCmd)
	var mode string
	serve := &cobra.Command{Use: "serve", Short: "Run the independent MCP Server in this terminal", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if mode != "foreground" && mode != "background" {
			return errors.New("invalid run mode")
		}
		return run.Serve(cmd.Context(), dir, mode)
	}}
	serve.Flags().StringVar(&mode, "mode", "foreground", "Runtime ownership: foreground or background")
	root.AddCommand(serve)
	var expectedInstance string
	runCmd := &cobra.Command{Use: "run [start|stop|restart|status]", Short: "Manage an independent background process", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "start":
			if err := run.Start(cmd.Context(), dir); err != nil {
				return err
			}
		case "stop":
			if err := run.StopInstance(cmd.Context(), dir, expectedInstance); err != nil {
				return err
			}
		case "restart":
			if err := run.Stop(cmd.Context(), dir); err != nil {
				return err
			}
			if err := run.Start(cmd.Context(), dir); err != nil {
				return err
			}
		case "status":
		default:
			return errors.New("unknown run action")
		}
		v, err := run.Status(cmd.Context(), dir)
		if err != nil {
			return err
		}
		return output(v)
	}}
	runCmd.Flags().StringVar(&expectedInstance, "instance-id", "", "Stop only the specified running process instance")
	root.AddCommand(runCmd)
	svc := &cobra.Command{Use: "service [install|uninstall|start|stop|restart|status|run]", Short: "Manage the native OS service (data survives uninstall)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !root.PersistentFlags().Changed("data-dir") && (runtime.GOOS == "windows" || system) {
			dir = run.SystemDir()
		}
		v, err := run.Service(dir, args[0], username, system)
		if err != nil {
			return err
		}
		return output(v)
	}}
	svc.Flags().StringVar(&username, "user", "", "Service account (Windows default: LocalService)")
	svc.Flags().BoolVar(&system, "system", false, "Use a system service instead of a POSIX user service")
	root.AddCommand(svc)
	apiCmd := &cobra.Command{Use: "api /admin/v1/path", Short: "Call the Admin API; JSON body from --file or stdin (-)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return call(cmd.Context(), dir, endpoint, method, args[0], input)
	}}
	apiCmd.Flags().StringVar(&method, "method", "GET", "HTTP method")
	apiCmd.Flags().StringVar(&input, "file", "", "JSON request file, or - for stdin")
	root.AddCommand(apiCmd)
	for _, resource := range []string{"instances", "entities", "approvals", "templates"} {
		resource := resource
		c := &cobra.Command{Use: resource + " [list|save|approve|deny] [id]", Short: "Manage " + resource, Args: cobra.RangeArgs(0, 2), RunE: func(cmd *cobra.Command, args []string) error {
			action := "list"
			if len(args) > 0 {
				action = args[0]
			}
			path := "/admin/v1/" + resource
			m := "GET"
			switch action {
			case "list":
			case "save":
				m = "POST"
				if len(args) == 2 {
					m = "PUT"
					path += "/" + args[1]
				}
			case "approve", "deny":
				if resource != "approvals" || len(args) != 2 {
					return errors.New("approval ID required")
				}
				m = "POST"
				path += "/" + args[1] + "/" + action
			default:
				return errors.New("unknown resource action")
			}
			return call(cmd.Context(), dir, endpoint, m, path, input)
		}}
		c.Flags().StringVar(&input, "file", "", "JSON request file, or - for stdin")
		root.AddCommand(c)
	}
	root.AddCommand(&cobra.Command{Use: "backup", Short: "Create an online consistent snapshot with matching keys", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return call(cmd.Context(), dir, endpoint, "POST", "/admin/v1/backups", "")
	}})
	root.AddCommand(&cobra.Command{Use: "restore BACKUP_DIRECTORY", Short: "Restore a validated snapshot while MCP Server is stopped; retain a recovery snapshot", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		v, err := run.Restore(cmd.Context(), dir, args[0])
		if err != nil {
			return err
		}
		return output(v)
	}})
	var credentials string
	var wait time.Duration
	bridge := &cobra.Command{Use: "bridge", Short: "Relay STDIO MCP to an existing MCP Server; never starts another database executor", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return api.Bridge(cmd.Context(), credentials, wait) }}
	bridge.Flags().StringVar(&credentials, "credentials", "", "Private JSON file containing url and entity token")
	_ = bridge.MarkFlagRequired("credentials")
	bridge.Flags().DurationVar(&wait, "wait", 20*time.Second, "Maximum time to wait for administrator approval")
	root.AddCommand(bridge)
	return root
}
func call(ctx context.Context, dir, endpoint, method, path, input string) error {
	var body io.Reader
	if input == "-" {
		body = os.Stdin
	} else if input != "" {
		f, err := os.Open(input)
		if err != nil {
			return err
		}
		defer f.Close()
		body = f
	}
	b, err := run.AdminRequest(ctx, dir, endpoint, os.Getenv("DBMCP_ADMIN_TOKEN"), strings.ToUpper(method), path, body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err
}
