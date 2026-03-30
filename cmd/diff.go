package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

type diffOutput struct {
	Added   []snapshotTool    `json:"added"`
	Removed []snapshotTool    `json:"removed"`
	Changed []changedToolDiff `json:"changed"`
}

type changedToolDiff struct {
	Name          string   `json:"name"`
	AddedFields   []string `json:"addedFields"`
	RemovedFields []string `json:"removedFields"`
	ChangedFields []string `json:"changedFields"`
	NewRequired   []string `json:"newRequired,omitempty"`
	NoLongerReq   []string `json:"noLongerRequired,omitempty"`
}

func init() {
	rootCmd.AddCommand(newDiffCmd())
}

func newDiffCmd() *cobra.Command {
	var format string
	var exitCode bool

	cmd := &cobra.Command{
		Use:   "diff <baseline.json> <current.json>",
		Short: "Compare two MCP schema snapshots",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			baseline, err := loadSnapshot(args[0])
			if err != nil {
				return err
			}
			current, err := loadSnapshot(args[1])
			if err != nil {
				return err
			}

			diff := compareSnapshots(baseline, current)

			if format != "" && format != "json" {
				return fmt.Errorf("--format must be empty or \"json\"")
			}

			if format == "json" {
				encoded, err := json.MarshalIndent(diff, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal diff: %w", err)
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(encoded)); err != nil {
					return err
				}
			} else {
				if err := writeDiff(cmd.OutOrStdout(), diff); err != nil {
					return err
				}
			}

			if exitCode && hasBreakingChanges(baseline, current, diff) {
				return exitCodeError{code: 1, err: errors.New("breaking schema changes detected")}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "Exit with code 1 if breaking changes are detected")
	cmd.Flags().StringVar(&format, "format", "", "Optional output format. Use \"json\" for machine-readable output")

	return cmd
}

func loadSnapshot(path string) (snapshotOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshotOutput{}, fmt.Errorf("read snapshot file: %w", err)
	}

	var snapshot snapshotOutput
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshotOutput{}, fmt.Errorf("decode snapshot file: %w", err)
	}

	return snapshot, nil
}

func compareSnapshots(baseline, current snapshotOutput) diffOutput {
	baselineMap := make(map[string]snapshotTool, len(baseline.Tools))
	for _, tool := range baseline.Tools {
		baselineMap[tool.Name] = tool
	}

	currentMap := make(map[string]snapshotTool, len(current.Tools))
	for _, tool := range current.Tools {
		currentMap[tool.Name] = tool
	}

	var diff diffOutput

	for name, tool := range currentMap {
		if _, ok := baselineMap[name]; !ok {
			diff.Added = append(diff.Added, tool)
		}
	}

	for name, tool := range baselineMap {
		if _, ok := currentMap[name]; !ok {
			diff.Removed = append(diff.Removed, tool)
		}
	}

	for name, baseTool := range baselineMap {
		currentTool, ok := currentMap[name]
		if !ok {
			continue
		}

		changed := compareToolSchemas(baseTool, currentTool)
		if len(changed.AddedFields) == 0 && len(changed.RemovedFields) == 0 && len(changed.ChangedFields) == 0 && len(changed.NewRequired) == 0 && len(changed.NoLongerReq) == 0 {
			continue
		}

		diff.Changed = append(diff.Changed, changed)
	}

	sort.Slice(diff.Added, func(i, j int) bool { return diff.Added[i].Name < diff.Added[j].Name })
	sort.Slice(diff.Removed, func(i, j int) bool { return diff.Removed[i].Name < diff.Removed[j].Name })
	sort.Slice(diff.Changed, func(i, j int) bool { return diff.Changed[i].Name < diff.Changed[j].Name })

	return diff
}

type schemaField struct {
	Type     string
	Required bool
}

func compareToolSchemas(baseline, current snapshotTool) changedToolDiff {
	baseFields := flattenSchemaFields(baseline.InputSchema)
	currentFields := flattenSchemaFields(current.InputSchema)

	diff := changedToolDiff{Name: baseline.Name}

	for field, base := range baseFields {
		currentField, ok := currentFields[field]
		if !ok {
			diff.RemovedFields = append(diff.RemovedFields, field)
			continue
		}
		if base.Type != currentField.Type {
			diff.ChangedFields = append(diff.ChangedFields, field)
		}
		if !base.Required && currentField.Required {
			diff.NewRequired = append(diff.NewRequired, field)
		}
		if base.Required && !currentField.Required {
			diff.NoLongerReq = append(diff.NoLongerReq, field)
		}
	}

	for field := range currentFields {
		if _, ok := baseFields[field]; !ok {
			diff.AddedFields = append(diff.AddedFields, field)
		}
	}

	sort.Strings(diff.AddedFields)
	sort.Strings(diff.RemovedFields)
	sort.Strings(diff.ChangedFields)
	sort.Strings(diff.NewRequired)
	sort.Strings(diff.NoLongerReq)

	return diff
}

func flattenSchemaFields(raw json.RawMessage) map[string]schemaField {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return map[string]schemaField{}
	}

	fields := make(map[string]schemaField)
	flattenSchemaNode("", schema, false, fields)
	return fields
}

func flattenSchemaNode(prefix string, node map[string]any, parentRequired bool, out map[string]schemaField) {
	properties, _ := node["properties"].(map[string]any)
	requiredSet := make(map[string]bool)
	if required, ok := node["required"].([]any); ok {
		for _, raw := range required {
			if name, ok := raw.(string); ok {
				requiredSet[name] = true
			}
		}
	}

	for name, rawChild := range properties {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}

		fieldPath := name
		if prefix != "" {
			fieldPath = prefix + "." + name
		}

		fieldType, _ := child["type"].(string)
		required := requiredSet[name] || parentRequired
		out[fieldPath] = schemaField{Type: fieldType, Required: required}
		flattenSchemaNode(fieldPath, child, required, out)
	}
}

func hasBreakingChanges(baseline, current snapshotOutput, diff diffOutput) bool {
	if len(diff.Removed) > 0 {
		return true
	}

	baselineMap := make(map[string]snapshotTool, len(baseline.Tools))
	for _, tool := range baseline.Tools {
		baselineMap[tool.Name] = tool
	}
	currentMap := make(map[string]snapshotTool, len(current.Tools))
	for _, tool := range current.Tools {
		currentMap[tool.Name] = tool
	}

	for _, changed := range diff.Changed {
		baseFields := flattenSchemaFields(baselineMap[changed.Name].InputSchema)
		currentFields := flattenSchemaFields(currentMap[changed.Name].InputSchema)
		for _, field := range changed.RemovedFields {
			if baseFields[field].Required {
				return true
			}
		}
		for _, field := range changed.ChangedFields {
			if baseFields[field].Required {
				return true
			}
		}
		if len(changed.NewRequired) > 0 {
			return true
		}
		for _, field := range changed.AddedFields {
			if currentFields[field].Required {
				return true
			}
		}
	}

	return false
}

func writeDiff(w io.Writer, diff diffOutput) error {
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	yellow := color.New(color.FgYellow)

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Changed) == 0 {
		_, err := fmt.Fprintln(w, "No schema changes.")
		return err
	}

	for _, tool := range diff.Added {
		if _, err := green.Fprintf(w, "+ %s\n", tool.Name); err != nil {
			return err
		}
	}
	for _, tool := range diff.Removed {
		if _, err := red.Fprintf(w, "- %s\n", tool.Name); err != nil {
			return err
		}
	}
	for _, changed := range diff.Changed {
		var parts []string
		if len(changed.AddedFields) > 0 {
			parts = append(parts, "added: "+strings.Join(changed.AddedFields, ", "))
		}
		if len(changed.RemovedFields) > 0 {
			parts = append(parts, "removed: "+strings.Join(changed.RemovedFields, ", "))
		}
		if len(changed.ChangedFields) > 0 {
			parts = append(parts, "changed: "+strings.Join(changed.ChangedFields, ", "))
		}
		if len(changed.NewRequired) > 0 {
			parts = append(parts, "new required: "+strings.Join(changed.NewRequired, ", "))
		}
		if len(changed.NoLongerReq) > 0 {
			parts = append(parts, "no longer required: "+strings.Join(changed.NoLongerReq, ", "))
		}
		if _, err := yellow.Fprintf(w, "~ %s (%s)\n", changed.Name, strings.Join(parts, "; ")); err != nil {
			return err
		}
	}

	return nil
}
