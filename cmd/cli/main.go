// Package main implements the FlowForge CLI tool.
//
// The CLI provides a user-friendly interface for managing workflows, runs,
// and server health via the FlowForge REST API.
//
// Usage:
//
//	flowforge workflow list
//	flowforge workflow get <id>
//	flowforge workflow create <file.yaml>
//	flowforge workflow run <id>
//	flowforge workflow delete <id>
//	flowforge run list
//	flowforge run status <id>
//	flowforge run logs <id>
//	flowforge run cancel <id>
//	flowforge version
//	flowforge health
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Build-time variables
// ---------------------------------------------------------------------------

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// ---------------------------------------------------------------------------
// API Client
// ---------------------------------------------------------------------------

// APIClient communicates with the FlowForge server REST API.
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
	APIKey     string
}

// NewAPIClient creates a client pointing at the given server URL.
func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		APIKey: apiKey,
	}
}

func (c *APIClient) newRequest(method, path string, body interface{}) (*http.Request, error) {
	url := c.BaseURL + path
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

func (c *APIClient) do(method, path string, body interface{}) (map[string]interface{}, error) {
	req, err := c.newRequest(method, path, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	// For 204 No Content, return empty map.
	if resp.StatusCode == http.StatusNoContent || len(respBody) == 0 {
		return map[string]interface{}{"status": "ok"}, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

func (c *APIClient) doStream(method, path string) (io.ReadCloser, error) {
	req, err := c.newRequest(method, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// Convenience helpers for common HTTP verbs.

func (c *APIClient) Get(path string) (map[string]interface{}, error) {
	return c.do("GET", path, nil)
}

func (c *APIClient) Post(path string, body interface{}) (map[string]interface{}, error) {
	return c.do("POST", path, body)
}

func (c *APIClient) Delete(path string) (map[string]interface{}, error) {
	return c.do("DELETE", path, nil)
}

// ---------------------------------------------------------------------------
// Table Printer
// ---------------------------------------------------------------------------

// printTable prints rows as an aligned table. The first row is treated as
// the header.
func printTable(headers []string, rows [][]string) {
	// Compute column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Build format string.
	fmtParts := make([]string, len(widths))
	for i, w := range widths {
		fmtParts[i] = fmt.Sprintf("%%-%ds", w)
	}
	format := strings.Join(fmtParts, "  ")

	// Print header.
	headerVals := make([]interface{}, len(headers))
	for i, h := range headers {
		headerVals[i] = strings.ToUpper(h)
	}
	fmt.Println(fmt.Sprintf(format, headerVals...))

	// Print separator.
	sepParts := make([]interface{}, len(widths))
	for i, w := range widths {
		sepParts[i] = strings.Repeat("-", w)
	}
	fmt.Println(fmt.Sprintf(format, sepParts...))

	// Print rows.
	for _, row := range rows {
		vals := make([]interface{}, len(widths))
		for i := range widths {
			if i < len(row) {
				vals[i] = row[i]
			} else {
				vals[i] = ""
			}
		}
		fmt.Println(fmt.Sprintf(format, vals...))
	}
}

// jsonStr safely extracts a string from a map field.
func jsonStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%.0f", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ---------------------------------------------------------------------------
// Root Command
// ---------------------------------------------------------------------------

func main() {
	var serverURL string
	var apiKey string

	rootCmd := &cobra.Command{
		Use:   "flowforge",
		Short: "FlowForge CLI -- distributed workflow orchestration",
		Long: `FlowForge is a distributed workflow orchestration engine.

Use this CLI to manage workflows, monitor runs, and interact with
the FlowForge server.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&serverURL, "server", envOrDefault("FLOWFORGE_SERVER", "http://localhost:8080"), "FlowForge server URL")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", envOrDefault("FLOWFORGE_API_KEY", ""), "API key for authentication")

	// Helper to build client from persistent flags.
	client := func() *APIClient {
		return NewAPIClient(serverURL, apiKey)
	}

	// ---- workflow commands -----------------------------------------------
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
		Aliases: []string{"wf"},
	}

	workflowListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all workflows",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Get("/api/v1/workflows")
			if err != nil {
				return err
			}

			workflows, _ := resp["workflows"].([]interface{})
			if len(workflows) == 0 {
				fmt.Println("No workflows found.")
				return nil
			}

			headers := []string{"ID", "Name", "Status", "Version", "Created"}
			var rows [][]string
			for _, w := range workflows {
				wf, ok := w.(map[string]interface{})
				if !ok {
					continue
				}
				rows = append(rows, []string{
					jsonStr(wf, "id"),
					jsonStr(wf, "name"),
					jsonStr(wf, "status"),
					jsonStr(wf, "version"),
					jsonStr(wf, "created_at"),
				})
			}
			printTable(headers, rows)
			fmt.Printf("\nTotal: %s workflow(s)\n", jsonStr(resp, "total"))
			return nil
		},
	}

	workflowGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show workflow details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Get("/api/v1/workflows/" + args[0])
			if err != nil {
				return err
			}

			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	workflowCreateCmd := &cobra.Command{
		Use:   "create <file>",
		Short: "Create a workflow from a YAML file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read file %q: %w", filePath, err)
			}

			// Parse YAML to a generic map, then send as JSON to the API.
			var definition map[string]interface{}
			if err := yaml.Unmarshal(data, &definition); err != nil {
				return fmt.Errorf("failed to parse YAML: %w", err)
			}

			resp, err := client().Post("/api/v1/workflows", definition)
			if err != nil {
				return err
			}

			fmt.Printf("Workflow created successfully.\n")
			fmt.Printf("  ID: %s\n", jsonStr(resp, "id"))
			return nil
		},
	}

	workflowRunCmd := &cobra.Command{
		Use:   "run <id>",
		Short: "Trigger a workflow execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Post("/api/v1/workflows/"+args[0]+"/run", nil)
			if err != nil {
				return err
			}

			fmt.Printf("Workflow execution triggered.\n")
			fmt.Printf("  Run ID: %s\n", jsonStr(resp, "run_id"))
			return nil
		},
	}

	workflowDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := client().Delete("/api/v1/workflows/" + args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Workflow %s deleted.\n", args[0])
			return nil
		},
	}

	workflowCmd.AddCommand(workflowListCmd, workflowGetCmd, workflowCreateCmd, workflowRunCmd, workflowDeleteCmd)

	// ---- run commands ---------------------------------------------------
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Manage workflow runs",
	}

	runListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent workflow runs",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Get("/api/v1/runs")
			if err != nil {
				return err
			}

			runs, _ := resp["runs"].([]interface{})
			if len(runs) == 0 {
				fmt.Println("No runs found.")
				return nil
			}

			headers := []string{"ID", "Workflow", "Status", "Trigger", "Started", "Completed"}
			var rows [][]string
			for _, r := range runs {
				run, ok := r.(map[string]interface{})
				if !ok {
					continue
				}
				rows = append(rows, []string{
					jsonStr(run, "id"),
					jsonStr(run, "workflow_id"),
					jsonStr(run, "status"),
					jsonStr(run, "trigger_type"),
					jsonStr(run, "started_at"),
					jsonStr(run, "completed_at"),
				})
			}
			printTable(headers, rows)
			fmt.Printf("\nTotal: %s run(s)\n", jsonStr(resp, "total"))
			return nil
		},
	}

	runStatusCmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show run status with task states",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Get("/api/v1/runs/" + args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Run: %s\n", jsonStr(resp, "id"))
			fmt.Printf("Status: %s\n", jsonStr(resp, "status"))
			fmt.Printf("Workflow: %s\n", jsonStr(resp, "workflow_id"))
			fmt.Printf("Started: %s\n", jsonStr(resp, "started_at"))
			fmt.Printf("Completed: %s\n", jsonStr(resp, "completed_at"))

			if errMsg := jsonStr(resp, "error"); errMsg != "" {
				fmt.Printf("Error: %s\n", errMsg)
			}

			// Print task states if available.
			tasks, _ := resp["tasks"].([]interface{})
			if len(tasks) > 0 {
				fmt.Println("\nTask States:")
				headers := []string{"Task ID", "Name", "Status", "Attempt", "Duration"}
				var rows [][]string
				for _, t := range tasks {
					task, ok := t.(map[string]interface{})
					if !ok {
						continue
					}
					rows = append(rows, []string{
						jsonStr(task, "task_id"),
						jsonStr(task, "name"),
						jsonStr(task, "status"),
						jsonStr(task, "attempt"),
						jsonStr(task, "duration"),
					})
				}
				printTable(headers, rows)
			}
			return nil
		},
	}

	runLogsCmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Stream run logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := client().doStream("GET", "/api/v1/runs/"+args[0]+"/logs")
			if err != nil {
				return err
			}
			defer body.Close()

			// For streaming, we try to parse line-delimited JSON.
			// If the response is a single JSON object, just print it.
			data, err := io.ReadAll(body)
			if err != nil {
				return fmt.Errorf("failed to read logs: %w", err)
			}

			// Try to parse as a JSON object with a logs array.
			var logResp map[string]interface{}
			if err := json.Unmarshal(data, &logResp); err == nil {
				logs, _ := logResp["logs"].([]interface{})
				if len(logs) == 0 {
					fmt.Println("No logs available yet.")
					return nil
				}
				for _, entry := range logs {
					logEntry, ok := entry.(map[string]interface{})
					if !ok {
						fmt.Println(entry)
						continue
					}
					ts := jsonStr(logEntry, "timestamp")
					level := jsonStr(logEntry, "level")
					msg := jsonStr(logEntry, "message")
					taskID := jsonStr(logEntry, "task_id")

					if taskID != "" {
						fmt.Printf("[%s] %s [%s] %s\n", ts, level, taskID, msg)
					} else {
						fmt.Printf("[%s] %s %s\n", ts, level, msg)
					}
				}
				return nil
			}

			// Fallback: print raw output.
			fmt.Print(string(data))
			return nil
		},
	}

	runCancelCmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a running workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Post("/api/v1/runs/"+args[0]+"/cancel", nil)
			if err != nil {
				return err
			}

			fmt.Printf("Run %s cancelled.\n", jsonStr(resp, "id"))
			return nil
		},
	}

	// Note: "run" as a noun (list/status/logs/cancel) collides with
	// "workflow run" as a verb. Cobra handles this correctly because they
	// live under different parent commands.
	runCmd.AddCommand(runListCmd, runStatusCmd, runLogsCmd, runCancelCmd)

	// ---- version command ------------------------------------------------
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("FlowForge CLI\n")
			fmt.Printf("  Version:    %s\n", version)
			fmt.Printf("  Commit:     %s\n", commit)
			fmt.Printf("  Built:      %s\n", buildDate)
			fmt.Println()

			// Also try to get server version.
			resp, err := client().Get("/version")
			if err != nil {
				fmt.Printf("  Server:     unreachable (%v)\n", err)
				return
			}
			fmt.Printf("  Server:     %s (commit %s)\n",
				jsonStr(resp, "version"),
				jsonStr(resp, "commit"),
			)
		},
	}

	// ---- health command -------------------------------------------------
	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Check server health",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client().Get("/health")
			if err != nil {
				fmt.Printf("Server is unreachable: %v\n", err)
				return err
			}

			status := jsonStr(resp, "status")
			fmt.Printf("Server: %s\n", status)
			fmt.Printf("Version: %s\n", jsonStr(resp, "version"))

			// Try readiness endpoint for more detail.
			readyResp, err := client().Get("/ready")
			if err == nil {
				checks, _ := readyResp["checks"].(map[string]interface{})
				if len(checks) > 0 {
					fmt.Println("\nDependency Checks:")
					headers := []string{"Service", "Status"}
					var rows [][]string
					for svc, st := range checks {
						s, _ := st.(string)
						rows = append(rows, []string{svc, s})
					}
					printTable(headers, rows)
				}
			}

			if status != "ok" {
				return fmt.Errorf("server is not healthy: %s", status)
			}
			return nil
		},
	}

	// ---- Assemble root command ------------------------------------------
	rootCmd.AddCommand(workflowCmd, runCmd, versionCmd, healthCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
