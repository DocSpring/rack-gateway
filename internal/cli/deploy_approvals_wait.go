package cli

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newDeployApprovalWaitCommand() *cobra.Command {
	var (
		racksFlag       string
		pollIntervalStr string
		autoApprove     bool
		notes           string
		loop            bool
	)

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for and optionally approve pending deploy approval requests",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			var racks []string
			if trimmed := strings.TrimSpace(racksFlag); trimmed != "" {
				for _, r := range strings.Split(trimmed, ",") {
					if r = strings.TrimSpace(r); r != "" {
						racks = append(racks, r)
					}
				}
			} else {
				rack, err := SelectedRack()
				if err != nil {
					return err
				}
				racks = []string{rack}
			}

			if len(racks) == 0 {
				return fmt.Errorf("no racks specified")
			}

			pollInterval, err := parseDurationFlag(pollIntervalStr, "poll-interval", false, time.Second)
			if err != nil {
				return err
			}

			type rackInfo struct {
				name string
			}
			rackInfos := make([]rackInfo, 0, len(racks))
			for _, rack := range racks {
				rackInfos = append(rackInfos, rackInfo{name: rack})
			}

			rackIndex := 0
			printWaitingMessage := func() error {
				if len(racks) == 1 {
					return writef(cmd.OutOrStdout(), "Waiting for pending deploy approval requests on rack: %s\n", racks[0])
				}
				return writef(cmd.OutOrStdout(), "Waiting for pending deploy approval requests on %d racks: %s\n", len(racks), strings.Join(racks, ", "))
			}

			if err := printWaitingMessage(); err != nil {
				return err
			}

			for {
				info := rackInfos[rackIndex]
				rackIndex = (rackIndex + 1) % len(rackInfos)

				var result struct {
					Requests []deployApprovalRequest `json:"deploy_approval_requests"`
				}
				if err := gatewayRequest(cmd, info.name, http.MethodGet, "/deploy-approval-requests?status=pending", nil, &result); err != nil {
					return err
				}

				if result.Requests == nil {
					return fmt.Errorf("unexpected API response format: missing 'deploy_approval_requests' field")
				}

				if len(result.Requests) > 0 {
					cfg, _, _ := LoadConfig()
					soundDone := make(chan struct{})
					go func() {
						defer close(soundDone)
						if err := playNotificationSound(cfg, info.name); err != nil {
							_ = writef(cmd.OutOrStdout(), "Warning: failed to play notification sound: %v\n", err)
						}
					}()

					req := result.Requests[0]
					if len(rackInfos) > 1 {
						if err := writef(cmd.OutOrStdout(), "\n📋 Deploy Approval Request Found on rack '%s':\n", info.name); err != nil {
							return err
						}
					} else {
						if err := writeLine(cmd.OutOrStdout(), "\n📋 Deploy Approval Request Found:"); err != nil {
							return err
						}
					}

					if err := writef(cmd.OutOrStdout(), "  ID: %s\n", req.PublicID); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Message: %s\n", req.Message); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Status: %s\n", req.Status); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Token: %s\n", req.TargetAPITokenName); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Created: %s\n", req.CreatedAt.Format(time.RFC3339)); err != nil {
						return err
					}

					if autoApprove {
						approved, err := approveDeployRequest(cmd, info.name, req.PublicID, strings.TrimSpace(notes))
						if err != nil {
							return err
						}

						statusLine := fmt.Sprintf("\n✅ Deploy approval request %s approved", approved.PublicID)
						if approved.ApprovalExpiresAt != nil {
							statusLine = fmt.Sprintf("%s (expires at %s)", statusLine, approved.ApprovalExpiresAt.UTC().Format(time.RFC3339))
						}
						if err := writeLine(cmd.OutOrStdout(), statusLine); err != nil {
							return err
						}
					} else {
						if err := writeLine(cmd.OutOrStdout(), "\nUse 'rack-gateway deploy-approval approve <id>' to approve this request."); err != nil {
							return err
						}
					}

					<-soundDone

					if !loop {
						return nil
					}

					if err := printWaitingMessage(); err != nil {
						return err
					}

					time.Sleep(pollInterval)
				}

				time.Sleep(pollInterval)
			}
		}),
	}

	cmd.Flags().StringVar(&racksFlag, "racks", "", "Comma-separated list of rack names to monitor (e.g., dev,staging,prod)")
	cmd.Flags().StringVar(&pollIntervalStr, "poll-interval", "1s", "Polling interval")
	cmd.Flags().BoolVar(&autoApprove, "approve", false, "Automatically approve the first pending request found")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional notes for approval (only used with --approve)")
	cmd.Flags().BoolVar(&loop, "loop", false, "Continue polling for more requests after displaying or approving one")

	return cmd
}

func playNotificationSound(cfg *Config, rack string) error {
	soundPref := "default"
	if cfg != nil {
		if cfg.NotificationSound != "" {
			soundPref = cfg.NotificationSound
		}
		if rack != "" {
			if gwCfg, ok := cfg.Gateways[rack]; ok && gwCfg.NotificationSound != "" {
				soundPref = gwCfg.NotificationSound
			}
		}
	}

	if soundPref == "disabled" {
		return nil
	}

	var soundFile string
	var cleanupFile bool

	if soundPref == "default" || soundPref == "" {
		tmpFile, err := os.CreateTemp("", "notification-*.mp3")
		if err != nil {
			return err
		}
		soundFile = tmpFile.Name()
		cleanupFile = true
		defer func() {
			if cleanupFile {
				_ = os.Remove(soundFile)
			}
		}()

		if _, err := tmpFile.Write(notificationSound); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			return err
		}
	} else {
		soundFile = soundPref
		if _, err := os.Stat(soundFile); err != nil {
			return fmt.Errorf("notification sound file not found: %w", err)
		}
	}

	var player *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		player = exec.Command("afplay", soundFile)
	case "linux":
		for _, candidate := range []string{"paplay", "aplay", "ffplay", "mpg123"} {
			if _, err := exec.LookPath(candidate); err == nil {
				if candidate == "ffplay" {
					player = exec.Command(candidate, "-nodisp", "-autoexit", soundFile)
				} else {
					player = exec.Command(candidate, soundFile)
				}
				break
			}
		}
		if player == nil {
			return fmt.Errorf("no audio player found (tried paplay, aplay, ffplay, mpg123)")
		}
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	return player.Run()
}
