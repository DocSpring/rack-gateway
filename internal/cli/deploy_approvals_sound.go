package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func playNotificationSound(cfg *Config, rack string) error {
	preference := resolveNotificationPreference(cfg, rack)
	if preference == "disabled" {
		return nil
	}

	soundFile, cleanup, err := ensureSoundFile(preference)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	volume := resolveSoundVolumePreference(cfg, rack)
	player, err := selectAudioPlayer(soundFile, volume)
	if err != nil {
		return err
	}
	return player.Run()
}

func resolveNotificationPreference(cfg *Config, rack string) string {
	const defaultPreference = "default"
	if cfg == nil {
		return defaultPreference
	}
	preference := cfg.NotificationSound
	if preference == "" {
		preference = defaultPreference
	}
	if rack == "" || cfg.Gateways == nil {
		return preference
	}
	if gwCfg, ok := cfg.Gateways[rack]; ok && gwCfg.NotificationSound != "" {
		return gwCfg.NotificationSound
	}
	return preference
}

func resolveSoundVolumePreference(cfg *Config, rack string) float64 {
	const defaultVolume = 0.6 // 60%
	if cfg == nil {
		return defaultVolume
	}
	volume := defaultVolume
	if cfg.SoundVolume != nil {
		volume = *cfg.SoundVolume
	}
	if rack == "" || cfg.Gateways == nil {
		return volume
	}
	if gwCfg, ok := cfg.Gateways[rack]; ok && gwCfg.SoundVolume != nil {
		return *gwCfg.SoundVolume
	}
	return volume
}

func ensureSoundFile(preference string) (string, func(), error) {
	if preference == "" || preference == "default" {
		return createTemporarySoundFile()
	}
	if _, err := os.Stat(preference); err != nil {
		return "", nil, fmt.Errorf("notification sound file not found: %w", err)
	}
	return preference, nil, nil
}

func createTemporarySoundFile() (string, func(), error) {
	tmpFile, err := os.CreateTemp("", "notification-*.mp3")
	if err != nil {
		return "", nil, err
	}

	if _, err := tmpFile.Write(notificationSound); err != nil {
		_ = tmpFile.Close()
		return "", nil, err
	}
	if err := tmpFile.Close(); err != nil {
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(tmpFile.Name())
	}
	return tmpFile.Name(), cleanup, nil
}

func selectAudioPlayer(soundFile string, volume float64) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		//nolint:gosec // G204: Command hardcoded, file path controlled
		return exec.Command("afplay", "-v", fmt.Sprintf("%.2f", volume), soundFile), nil
	case "linux":
		return linuxAudioPlayer(soundFile, volume)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func linuxAudioPlayer(soundFile string, volume float64) (*exec.Cmd, error) {
	for _, candidate := range []string{"paplay", "aplay", "ffplay", "mpg123"} {
		if _, err := exec.LookPath(candidate); err == nil {
			//nolint:gosec // G204: Command hardcoded, file path controlled
			switch candidate {
			case "paplay":
				return exec.Command(candidate, "--volume", fmt.Sprintf("%.0f", volume*65536), soundFile), nil
			case "ffplay":
				volStr := fmt.Sprintf("%.0f", volume*100)
				return exec.Command(candidate, "-nodisp", "-autoexit", "-volume", volStr, soundFile), nil
			case "mpg123":
				return exec.Command(candidate, "-f", fmt.Sprintf("%.0f", volume*32768), soundFile), nil
			default:
				return exec.Command(candidate, soundFile), nil
			}
		}
	}
	return nil, fmt.Errorf("no audio player found (tried paplay, aplay, ffplay, mpg123)")
}
