package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/anutron/iris/internal/verbs"
)

func printJSON(cmd *cobra.Command, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(body))
	return nil
}

// Verb seams: tests replace these to observe what the CLI passes through.
var (
	dogfoodClaimVerb        = verbs.DogfoodClaim
	dogfoodRenewVerb        = verbs.DogfoodRenew
	dogfoodReleaseVerb      = verbs.DogfoodRelease
	dogfoodForceReleaseVerb = verbs.DogfoodForceRelease
	dogfoodQueueMoveVerb    = verbs.DogfoodQueueMove
)

func newDogfoodCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dogfood",
		Short: "Dogfood lease and queue: claim, renew, release, force-release, queue-move",
		Long: `The dogfood lease serializes iris set-dogfood: one task holds it at a time
and the rest wait in an ordered queue (FIFO by default). Agents normally call
the iris_dogfood_* MCP tools; these subcommands drive the same verbs from a
terminal.`,
	}
	root.AddCommand(newDogfoodClaimCmd(), newDogfoodRenewCmd(), newDogfoodReleaseCmd(),
		newDogfoodForceReleaseCmd(), newDogfoodQueueMoveCmd())
	return root
}

func newDogfoodClaimCmd() *cobra.Command {
	var taskID string
	var wait, position int
	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Claim the dogfood lease, or join its queue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for dogfood claim: %w", err)
			}
			res, err := dogfoodClaimVerb(cmd.Context(), client, verbs.DogfoodClaimInput{TaskID: taskID, WaitSeconds: wait, Position: position})
			if err != nil {
				return err
			}
			return printJSON(cmd, res)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "argus task ID claiming the lease (required)")
	cmd.Flags().IntVar(&wait, "wait", 0, "seconds to wait for a grant (0-25)")
	cmd.Flags().IntVar(&position, "position", 0, "cut in line at this 1-based queue position (default: append)")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newDogfoodRenewCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "renew",
		Short: "Answer a check-in: you still hold and use the lease",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for dogfood renew: %w", err)
			}
			res, err := dogfoodRenewVerb(cmd.Context(), client, taskID)
			if err != nil {
				return err
			}
			return printJSON(cmd, res)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "argus task ID of the holder (required)")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newDogfoodReleaseCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Release the lease (or leave the queue)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for dogfood release: %w", err)
			}
			res, err := dogfoodReleaseVerb(cmd.Context(), client, taskID)
			if err != nil {
				return err
			}
			return printJSON(cmd, res)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "argus task ID releasing the lease (required)")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newDogfoodForceReleaseCmd() *cobra.Command {
	var taskID, path, reason string
	cmd := &cobra.Command{
		Use:   "force-release",
		Short: "Evict the current holder (human/coordinator override)",
		Long: `force-release releases the current holder whatever its state, records --reason
in the lease history, messages the evicted holder, and grants the lease to the
next live queued task. It works even when argus is unreachable (messaging is
then skipped). With neither --task nor --path it targets iris itself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskID != "" && path != "" {
				return errors.New("--task and --path are mutually exclusive")
			}
			// Argus being unreachable must not block an eviction.
			client, _ := newCLIClient(cmd.Context())
			res, err := dogfoodForceReleaseVerb(cmd.Context(), client, verbs.DogfoodForceReleaseInput{TaskID: taskID, Path: path, Reason: reason})
			if err != nil {
				return err
			}
			return printJSON(cmd, res)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "(optional) argus task ID identifying the source repo")
	cmd.Flags().StringVar(&path, "path", "", "(optional) source repo path")
	cmd.Flags().StringVar(&reason, "reason", "", "why the holder is being evicted (required)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newDogfoodQueueMoveCmd() *cobra.Command {
	var taskID, path string
	cmd := &cobra.Command{
		Use:   "queue-move <task> <position>",
		Short: "Move a queued task to a 1-based position",
		Long: `queue-move reorders the dogfood lease queue. The holder is never affected and
moved tasks are not messaged. --task/--path identify the source repo (default:
iris itself).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, err := strconv.Atoi(args[1])
			if err != nil || pos < 1 {
				return fmt.Errorf("position must be a positive integer, got %q", args[1])
			}
			if taskID != "" && path != "" {
				return errors.New("--task and --path are mutually exclusive")
			}
			client, err := newCLIClient(cmd.Context())
			if err != nil {
				return fmt.Errorf("argus client required for dogfood queue-move: %w", err)
			}
			res, err := dogfoodQueueMoveVerb(cmd.Context(), client, verbs.DogfoodQueueMoveInput{TaskID: taskID, Path: path, MoveTaskID: args[0], Position: pos})
			if err != nil {
				return err
			}
			return printJSON(cmd, res)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "(optional) argus task ID identifying the source repo")
	cmd.Flags().StringVar(&path, "path", "", "(optional) source repo path")
	return cmd
}
