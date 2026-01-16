package externalprograms

import (
	"os/exec"
	"runtime"

	"github.com/Hellseher/go-shellquote"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// Execute runs an external program for a torrent.
func Execute(program *models.ExternalProgram, torrent *qbt.Torrent) {
	// Check params
	if program == nil || torrent == nil {
		log.Warn().Any("program", program).Any("torrent", torrent).Msg("Skipping external program execution - invalid params")
		return
	}

	// Build command arguments
	args := buildArguments(program, torrent)

	// Build command
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmdArgs := []string{"/c", "start", ""}
		if program.UseTerminal {
			// Windows: Use cmd.exe /c start cmd /k to open a new visible terminal window
			// Empty string after "start" prevents quoted paths from being interpreted as window title
			// When using "cmd /k", we need to pass the program path and args as separate arguments
			// exec.Command will handle quoting for CreateProcess, and cmd.exe will receive them properly
			cmdArgs = append(cmdArgs, "cmd", "/k")
		} else {
			// Windows: Use 'start' to launch GUI apps properly (detached from parent process)
			// Empty string after "start" prevents quoted paths from being interpreted as window title
			// Pass program path and args as separate arguments - exec.Command handles quoting
			cmdArgs = append(cmdArgs, "/b")
		}
		cmdArgs = append(cmdArgs, program.Path)
		cmdArgs = append(cmdArgs, args...)
		cmd = exec.Command("cmd.exe", cmdArgs...)
	} else {
		if program.UseTerminal {
			// Unix/Linux: Build command string and spawn in a terminal
			// Use shellquote library to properly escape for Unix shells
			allArgs := append([]string{program.Path}, args...)
			fullCmd := shellquote.Join(allArgs...)
			// Try to find an available terminal emulator and spawn the command in it
			cmd = createTerminalCommand(fullCmd)
		} else {
			// Launch directly without terminal (for GUI apps or background processes)
			cmd = exec.Command(program.Path, args...)
		}
	}

	// Log the full command being executed for debugging
	log.Debug().
		Str("hash", torrent.Hash).
		Str("programName", program.Name).
		Int("programId", program.ID).
		Str("path", program.Path).
		Strs("args", args).
		Strs("command", cmd.Args).
		Msg("Executing external program")

	// Execute the command in a goroutine so it doesn't block qui from shutting down while the external program runs.
	go func() {
		if runtime.GOOS == "windows" {
			// Windows: Use Run() which waits for cmd.exe to complete
			// The 'start' command will spawn the process and cmd.exe will exit quickly
			if err := cmd.Run(); err != nil {
				// Log the error for debugging, but note that 'start' command
				// may return non-zero exit code even on successful spawn
				log.Error().
					Str("hash", torrent.Hash).
					Str("programName", program.Name).
					Int("programId", program.ID).
					Str("path", program.Path).
					Strs("args", args).
					Strs("command", cmd.Args).
					Msg("Failed to launch external program  (cmd.exe exited with error which can be normal for 'start' command)")
			}
		} else {
			// Unix/Linux: Start the terminal emulator or direct process
			if err := cmd.Start(); err != nil {
				log.Error().
					Err(err).
					Str("hash", torrent.Hash).
					Str("programName", program.Name).
					Int("programId", program.ID).
					Str("path", program.Path).
					Strs("args", args).
					Strs("command", cmd.Args).
					Msg("Failed to start external program")
			}

			// Wait for the process to prevent zombie processes
			// This runs in the goroutine, so it won't block qui
			if err := cmd.Wait(); err != nil {
				// Terminal emulators may exit with non-zero status
				log.Debug().
					Err(err).
					Str("hash", torrent.Hash).
					Str("programName", program.Name).
					Int("programId", program.ID).
					Str("path", program.Path).
					Strs("args", args).
					Strs("command", cmd.Args).
					Msg("Failed to execute external program")
			}
		}
	}()

	msg := "Executed external program"
	if runtime.GOOS == "windows" {
		msg = "Launched external program (using cmd.exe /c start -> outcome unknown)"
	}
	log.Debug().
		Str("hash", torrent.Hash).
		Str("programName", program.Name).
		Int("programId", program.ID).
		Str("path", program.Path).
		Strs("args", args).
		Strs("command", cmd.Args).
		Msg(msg)
}
