package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check Daytona connectivity, snapshot config, and credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}
			hasIssue := false
			apiKey := sandbox.ResolveAPIKey(cfg)
			if apiKey != "" {
				fmt.Println("✓ Daytona API key is configured")
			} else {
				fmt.Println("✗ Daytona API key missing")
				fmt.Println("  Run: amux auth login")
				hasIssue = true
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				fmt.Printf("✗ Daytona client error: %v\n", err)
				return nil
			}

			if _, err := client.Volume.Get(sandbox.CredentialsVolumeName, true); err != nil {
				fmt.Println("✗ Credentials volume missing")
				fmt.Println("  Run: amux setup")
				hasIssue = true
			} else {
				fmt.Println("✓ Credentials volume ready")
			}

			snapshotName := sandbox.ResolveSnapshotID(cfg)
			if snapshotName != "" {
				if snap, err := client.Snapshot.Get(snapshotName); err == nil {
					fmt.Printf("✓ Snapshot \"%s\" is %s\n", snap.Name, snap.State)
				} else {
					fmt.Printf("✗ Snapshot \"%s\" not found\n", snapshotName)
					fmt.Println("  Run: amux setup")
					hasIssue = true
				}
			} else {
				fmt.Println("• No default snapshot configured")
				fmt.Println("  Run: amux setup")
				hasIssue = true
			}

			if !hasIssue {
				fmt.Println("All checks passed. You can run `amux sandbox run claude`.")
			}
			return nil
		},
	}
	return cmd
}
