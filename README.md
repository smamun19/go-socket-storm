# go-socket-storm ⚡️

A simple command-line tool written in Go for stress/load testing WebSocket servers. It allows you to establish a configurable number of concurrent connections at a specific rate and hold them open, optionally for a set duration, while monitoring connection success, failures, and basic data transfer.

## Features

- **Concurrent Connections:** Establish and maintain a large number of simultaneous WebSocket connections.
- **Connection Rate Limiting:** Control the rate at which new connections are established.
- **Test Duration:** Run the test for a specific duration or until manually interrupted.
- **Graceful Shutdown:** Handles `SIGINT` (Ctrl+C) and `SIGTERM` for cleanly stopping the test and closing connections.
- **Automatic Reconnection:** Workers automatically attempt to reconnect if a connection fails or drops unexpectedly (with configurable delay).
- **Real-time Statistics:** Prints periodic status updates on active, successful, and failed connections, and total bytes read.
- **Final Summary:** Provides aggregate statistics at the end of the test.
- **Verbose Logging:** Option to enable detailed logging for individual connection events and errors.

## Installation

1.  **Ensure Go is installed:** You need Go (version 1.18 or later recommended) installed on your system.
2.  **Install using `go install` (Recommended):**

    ```bash
    go install github.com/smamun19/go-socket-storm@latest
    ```

    This will download, compile, and place the `go-socket-storm` binary in your `$GOPATH/bin` directory. Make sure this directory is in your system's `PATH`.

3.  **Alternatively, build from source:**

    ```bash
    # Clone the repository
    git clone https://github.com/smamun19/go-socket-storm.git
    cd go-socket-storm

    # Build the binary
    go build -o go-socket-storm .

    # Now you can run it directly:
    ./go-socket-storm --url ws://...
    ```

## Usage

Run the tool from your command line, specifying the target WebSocket URL and desired options:

```bash
go-socket-storm [flags]
```

### Flags

_(This section details the command-line flags)_

- `--url URL` (**Required**): The WebSocket server URL to connect to (e.g., `ws://localhost:8080/ws`, `wss://example.com/socket`).
- `-c CONCURRENCY` (Optional): Total number of concurrent connections to establish. (Default: `100`)
- `-r RATE` (Optional): Rate of new connections to establish per second. (Default: `10`)
- `-d DURATION` (Optional): Test duration in seconds (e.g., `30`, `120`). If `0`, the test runs until the target concurrency is reached and then waits indefinitely until interrupted (Ctrl+C). (Default: `0`)
- `-v` (Optional): Enable verbose logging. Shows detailed connection errors, close events, received text messages, and pongs. (Default: `false`)

### Examples

1.  **Basic Test:** Connect 100 clients at 10 connections/sec to a local server and run until interrupted:

    ```bash
    go-socket-storm --url ws://localhost:8080/ws
    ```

2.  **Higher Load & Duration:** Connect 500 clients at 50 connections/sec, run for 60 seconds, with verbose logging:

    ```bash
    go-socket-storm --url wss://echo.websocket.org --c 500 --r 50 -d 60 -v
    ```

    _(Note: Public echo servers might have rate limits)_

3.  **Ramp up and Hold:** Connect 1000 clients at 100 connections/sec and keep them connected until Ctrl+C is pressed:
    ```bash
    go-socket-storm --url ws://my-server.com/api -c 1000 -r 100
    ```

## Output Explanation

- **Initial Log:** Shows the configuration the test is running with.
- **Periodic Status:** Every 5 seconds, a status line is printed:
  - `Active`: Current number of established and actively maintained connections.
  - `Succeeded`: Total number of connections successfully established so far (including reconnections).
  - `Failed`: Total number of _failed connection attempts_ (initial dial or reconnect attempts). Note that one worker might contribute multiple failures if it keeps failing to reconnect.
  - `BytesRead`: Total bytes received across all connections.
- **Verbose Logs (`-v`):** Detailed messages about connection failures, unexpected closes, successful pings after timeouts, received text messages, and pong replies.
- **Shutdown:** Messages indicating shutdown initiation and waiting for workers.
- **Final Summary:** After the test finishes (duration reached or interrupted and workers stopped):
  - `Duration`: Total time the test ran.
  - `Successful Connections`: Final count of successful connection establishments.
  - `Failed Connections`: Final count of failed connection attempts.
  - `Total Bytes Read`: Final count of bytes received.

## How it Works

The tool spawns worker goroutines. A main loop attempts to launch new workers at the rate specified by `-r` using a `time.Ticker`, up to the concurrency limit `-c`.

Each worker (`worker` function):

1.  Attempts to connect to the specified `--url`.
2.  If connection fails, it enters a retry loop (`reconnectLoop`) with a delay (`reconnectDelay`), incrementing the global `failedConnections` counter on each failure.
3.  If connection succeeds, it increments `successfulConnections` and `activeConnections`.
4.  It then enters a loop to read messages (`conn.ReadMessage()`).
5.  It uses `SetReadDeadline` to implement a timeout. If a read times out, it sends a Ping.
6.  If the Ping fails or any other read error occurs (like the connection dropping), it jumps back to the `reconnectLoop` to try establishing a _new_ connection (after potentially incrementing `failedConnections` again for the ping failure).
7.  If a message is read successfully, `totalBytesRead` is updated, and the read deadline is reset.
8.  Workers listen for a global `shutdown` signal to gracefully close their connection and exit.
9.  `sync.WaitGroup` is used to ensure the main program waits for all workers to finish before exiting.
10. A separate goroutine (`printStats`) periodically prints the global counters.

## Dependencies

- [github.com/gorilla/websocket](https://github.com/gorilla/websocket): The core library used for WebSocket client connections. _(Added link for convenience)_

## License

This project is licensed under the MIT License - see the `LICENSE` file for details.
