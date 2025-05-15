package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// forwardAndLogStdin reads from proxy's stdin, logs it, and writes to target's stdin
func forwardAndLogStdin(proxyStdin io.Reader, targetStdin io.WriteCloser, logFile *os.File, wg *sync.WaitGroup) {
	defer wg.Done()
	buffer := make([]byte, 4096) // Use buffer for efficient reading

	for {
		n, err := proxyStdin.Read(buffer)
		if n > 0 {
			// Write to log file with ISO timestamp and "in:  " prefix
			timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
			logData := append([]byte(timestamp+" in:  "), buffer[:n]...)
			_, logErr := logFile.Write(logData)
			if logErr != nil {
				log.Printf("Error writing to log file: %v", logErr)
			}
			logFile.Sync() // Flush immediately

			// Write to target process stdin
			_, writeErr := targetStdin.Write(buffer[:n])
			if writeErr != nil {
				log.Printf("Error writing to target stdin: %v", writeErr)
				break
			}
		}

		if err != nil {
			// Log the error but continue processing
			log.Printf("STDIN Forwarding Error: %v", err)
			break
		}
	}

	// Close target stdin when proxy stdin closes
	if closeErr := targetStdin.Close(); closeErr != nil {
		log.Printf("Error closing target stdin: %v", closeErr)
	}
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	_, err := logFile.WriteString(timestamp + " --- STDIN stream closed to target ---\n")
	if err != nil {
		log.Printf("Error writing to log file: %v", err)
	}
	logFile.Sync() // Ensure log is flushed
}

// forwardAndLogStream reads from target's stdout/stderr, logs it, and writes to proxy's stdout
func forwardAndLogStream(target io.Reader, proxy io.Writer, logFile *os.File, prefix string, wg *sync.WaitGroup) {
	defer wg.Done()
	reader := bufio.NewReader(target)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
			if strings.HasPrefix(line, prefix+" ") {
				// already has prefix, write log directly (still add timestamp)
				if !strings.HasSuffix(line, "\n") {
					logFile.WriteString(timestamp + " " + line + "\n")
				} else {
					logFile.WriteString(timestamp + " " + line)
				}
			} else {
				// no prefix, add prefix and write log
				if !strings.HasSuffix(line, "\n") {
					logFile.WriteString(timestamp + " " + prefix + line + "\n")
				} else {
					logFile.WriteString(timestamp + " " + prefix + line)
				}
			}
			logFile.Sync()
			// write to proxy
			proxy.Write([]byte(line))
		}
		if err != nil {
			break
		}
	}
}

func main() {
	// Check if a command was provided
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	// Create log file path in same directory as executable
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error getting executable path: %v", err)
	}
	timestamp := time.Now().UTC().Format("2006-01-02_150405")
	logFileName := fmt.Sprintf("stdio-%s.log", timestamp)
	logFilePath := filepath.Join(filepath.Dir(exePath), logFileName)

	// Open log file in append mode
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Error creating log file: %v", err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			log.Printf("Error closing log file: %v", err)
		}
	}()

	// Detect OS and wrap command if needed
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Use cmd.exe /C for Windows built-in commands
		allArgs := append([]string{"/C", command}, args...)
		cmd = exec.Command("cmd.exe", allArgs...)
	} else {
		// Use sh -c for Unix-like systems
		fullCmd := append([]string{command}, args...)
		cmd = exec.Command("sh", "-c", strings.Join(fullCmd, " "))
	}

	// Set up pipes for stdin, stdout and stderr
	pipeStdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Error creating stdin pipe: %v", err)
	}

	pipeStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Error creating stdout pipe: %v", err)
	}

	pipeStderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("Error creating stderr pipe: %v", err)
	}

	// Start the target process
	if err := cmd.Start(); err != nil {
		log.Printf("Error starting command: %v", err)
		// Try to log the error too
		_, logErr := logFile.WriteString(fmt.Sprintf("!!! Logger Error: %v\n", err))
		if logErr != nil {
			log.Printf("Error writing to log file: %v", logErr)
		}
		logFile.Sync()
		os.Exit(1) // Indicate logger failure
	}

	var wg sync.WaitGroup

	// Start forwarding stdin
	wg.Add(1)
	go forwardAndLogStdin(os.Stdin, pipeStdin, logFile, &wg)

	// Start forwarding stdout
	wg.Add(1)
	go forwardAndLogStream(pipeStdout, os.Stdout, logFile, "out: ", &wg)

	// Start forwarding stderr
	wg.Add(1)
	go forwardAndLogStream(pipeStderr, os.Stderr, logFile, "err: ", &wg)

	// Wait for all goroutines to finish
	wg.Wait()

	// Wait for the command to finish
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			log.Printf("Command finished with error: %v", err)
			// Try to log the error too
			_, logErr := logFile.WriteString(fmt.Sprintf("!!! Command Error: %v\n", err))
			if logErr != nil {
				log.Printf("Error writing to log file: %v", logErr)
			}
			logFile.Sync()
			exitCode = 1
		}
	}

	// Ensure the process is terminated if it's still running (e.g., if logger crashed)
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("Error killing process: %v", err)
		}
	}

	os.Exit(exitCode)
}
