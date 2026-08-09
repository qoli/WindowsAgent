package actioncheck

import (
	"fmt"
	"io"
	"strings"
)

// WriteText writes a stable human-readable report.
func WriteText(writer io.Writer, result Result) error {
	if result.Valid {
		_, err := fmt.Fprintf(
			writer,
			"Action dependency check passed: %d Rules, %d Actions, %d dependencies\n",
			result.RuleCount,
			result.ActionCount,
			result.DependencyCount,
		)
		return err
	}
	if _, err := fmt.Fprintf(writer, "Action dependency check failed: %d issues\n", len(result.Issues)); err != nil {
		return err
	}
	for _, issue := range result.Issues {
		location := issue.Path
		if location == "" {
			location = issue.RuleID
		}
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, issue.Line, issue.Column)
		}
		if _, err := fmt.Fprintf(writer, "%s: %s: %s", location, issue.Code, issue.Message); err != nil {
			return err
		}
		if issue.ActionID != "" {
			if _, err := fmt.Fprintf(writer, " [action=%s", issue.ActionID); err != nil {
				return err
			}
			if issue.Primitive != "" {
				if _, err := fmt.Fprintf(writer, " primitive=%s", issue.Primitive); err != nil {
					return err
				}
			}
			if issue.Dependency != "" {
				if _, err := fmt.Fprintf(writer, " dependency=%s", issue.Dependency); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(writer, "]"); err != nil {
				return err
			}
		}
		if len(issue.Chain) > 0 {
			if _, err := fmt.Fprintf(writer, "\n  chain: %s", strings.Join(issue.Chain, " -> ")); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	return nil
}
