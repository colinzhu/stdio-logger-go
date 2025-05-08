# STDIO Logger Go

A Go implementation of a command wrapper that logs all STDIN/STDOUT/STDERR traffic while passing it through verbatim.

## Features

- Logs all input and output of wrapped commands
- Preserves exact data flow between streams
- Creates log file in same directory as executable
- Properly handles process termination and cleanup

## Building

To build the project, run:
```bash
$ go build -o stdio-logger-go
```

This will create a binary named `stdio-logger-go` in current directory.

## Usage

Basic usage:
```bash
$ ./stdio-logger-go <command> [args...]
```

Example:
```bash
$ ./stdio-logger-go echo "Hello World"
```

The program will create a log file named `stdio_io.log` in the same directory as the executable.

## Output

The log file will contain entries with prefixes:
- `输入:` for standard input
- `输出:` for standard output
- `STDERR:` for standard error