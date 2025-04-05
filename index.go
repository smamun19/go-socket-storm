package main

import (
	"flag"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var (
	wsUrl       = flag.String("url", "", "WebSocket server URL (e.g., ws://localhost:8080/ws)")
	concurrency = flag.Int("c", 100, "Total concurrent connections to establish")
	rate        = flag.Int("r", 10, "New connections per second")
	duration    = flag.Int("d", 0, "Test duration (e.g., 30s, 5m). If 0, runs until concurrency is reached or interrupted.")
	verbose     = flag.Bool("v", false, "Enable verbose logging for connection errors")
)

var (
	successfulConnections int64
	failedConnections     int64
	activeConnections     int64
	totalBytesRead        int64
)

var shutdown chan struct{} = make(chan struct{})

func main() {
	flag.Parse()

	if *wsUrl == "" {
		log.Fatal("WebSocket URL (--url) is required")
	}
	if *concurrency <= 0 {
		log.Fatal("Concurrency (--c) must be positive")
	}
	if *rate <= 0 {
		log.Fatal("Rate (--r) must be positive")
	}

	u, err := url.Parse(*wsUrl)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		log.Fatalf("Invalid WebSocket URL: %s. Error: %v", *wsUrl, err)
	}

	log.Printf("Starting WebSocket Load Tester:")
	log.Printf("  URL: %s", *wsUrl)
	log.Printf("  Total Connections: %d", *concurrency)
	log.Printf("  Connection Rate: %d/s", *rate)
	if *duration > 0 {
		log.Printf("  Test Duration: %d", *duration)
	} else {
		log.Printf("  Test Duration: Unlimited (until concurrency reached or interrupted)")
	}
	log.Printf("------------------------------------")

	var wg sync.WaitGroup

	ticker := time.NewTicker(time.Second / time.Duration(*rate))
	defer ticker.Stop()

	done := make(chan struct{})

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nShutdown signal received, stopping workers...")
		close(shutdown)

		if *duration > 0 {
			close(done)
		}
	}()

	if *duration > 0 {

		go func() {
			time.Sleep(time.Duration(*duration))
			log.Println("\nTest duration reached, stopping workers...")
			close(shutdown)
			close(done)
		}()
	}

	go printStats()

	establishedConnections := 0
	startTime := time.Now()

	for establishedConnections < *concurrency {
		select {
		case <-ticker.C:
			wg.Add(1)
			go worker(*wsUrl, &wg)
			establishedConnections++
		case <-shutdown:
			log.Printf("Stopping connection ramp-up due to shutdown signal.")
			goto endLoop

		}
	}
endLoop:
	if *duration > 0 {
		log.Printf("Reached target connection count (%d). Waiting for test duration (%d) or interrupt...", establishedConnections, *duration)
		<-done
	} else {
		log.Printf("Reached target connection count (%d). Waiting for interrupt (Ctrl+C)...", establishedConnections)

		select {
		case <-shutdown:

		default:

			<-shutdown
		}
	}

	log.Println("Waiting for active connections to close...")
	wg.Wait()
	endTime := time.Now()

	log.Println("------------------------------------")
	log.Printf("Test Finished.")
	log.Printf("Duration: %s", endTime.Sub(startTime).Round(time.Millisecond))
	log.Printf("Successful Connections: %d", atomic.LoadInt64(&successfulConnections))
	log.Printf("Failed Connections: %d", atomic.LoadInt64(&failedConnections))
	log.Printf("Total Bytes Read: %d", atomic.LoadInt64(&totalBytesRead))

}

func worker(url string, wg *sync.WaitGroup) {
	defer wg.Done()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in worker: %v", r)
		}
	}()

	const maxReconnectAttempts = 0 // 0 = unlimited
	const reconnectDelay = 2 * time.Second

	var reconnectAttempts int
	var conn *websocket.Conn
	var err error

reconnectLoop:
	for {
		select {
		case <-shutdown:
			if *verbose {
				log.Println("Worker skipping connection due to shutdown signal.")
			}
			return
		default:
		}

		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			atomic.AddInt64(&failedConnections, 1)
			if *verbose {
				log.Printf("Connection failed: %v", err)
			}
			if maxReconnectAttempts > 0 && reconnectAttempts >= maxReconnectAttempts {
				return
			}
			reconnectAttempts++
			time.Sleep(reconnectDelay)
			continue reconnectLoop
		}

		break
	}

	atomic.AddInt64(&successfulConnections, 1)
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	for {
		select {
		case <-shutdown:
			if *verbose {
				log.Printf("Worker [%s] received shutdown. Closing connection.", conn.LocalAddr())
			}
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			time.Sleep(500 * time.Millisecond)
			return
		default:
		}

		messageType, p, err := conn.ReadMessage()

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) ||
				websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				if *verbose {
					log.Printf("Worker [%s] connection closed: %v", conn.LocalAddr(), err)
				}
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				err = conn.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					if *verbose {
						log.Printf("Worker [%s] ping failed: %v", conn.LocalAddr(), err)
					}
					goto reconnectLoop
				}
				conn.SetReadDeadline(time.Now().Add(10 * time.Second))
				continue
			} else {
				if *verbose {
					log.Printf("Worker [%s] unhandled error: %v", conn.LocalAddr(), err)
				}
			}
			goto reconnectLoop
		}

		atomic.AddInt64(&totalBytesRead, int64(len(p)))

		if *verbose && messageType == websocket.TextMessage {
			log.Printf("Worker [%s] received: %s", conn.LocalAddr(), string(p))
		}

		if messageType == websocket.PongMessage && *verbose {
			log.Printf("Worker [%s] received Pong", conn.LocalAddr())
		}

		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	}
}

func printStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("Status => Active: %d, Succeeded: %d, Failed: %d, BytesRead: %d",
				atomic.LoadInt64(&activeConnections),
				atomic.LoadInt64(&successfulConnections),
				atomic.LoadInt64(&failedConnections),
				atomic.LoadInt64(&totalBytesRead),
			)
		case <-shutdown:
			return
		}
	}
}
