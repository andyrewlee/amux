package cli

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner provides a simple terminal spinner for long-running operations.
type Spinner struct {
	message  string
	done     chan struct{}
	mu       sync.Mutex
	active   bool
	stopOnce sync.Once
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		done:    make(chan struct{}),
	}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	go func() {
		frame := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.active {
					fmt.Printf("\r%s %s", spinnerFrames[frame%len(spinnerFrames)], s.message)
					frame++
				}
				s.mu.Unlock()
			}
		}
	}()
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	s.doStop("")
}

// StopWithMessage stops the spinner and prints a final message.
func (s *Spinner) StopWithMessage(message string) {
	s.doStop(message)
}

// doStop handles the actual stop logic with sync.Once to prevent double-close panic.
func (s *Spinner) doStop(message string) {
	s.mu.Lock()
	wasActive := s.active
	s.active = false
	s.mu.Unlock()

	s.stopOnce.Do(func() {
		close(s.done)
	})

	// Clear line and optionally print message
	if wasActive {
		if message != "" {
			fmt.Printf("\r\033[K%s\n", message)
		} else {
			fmt.Printf("\r\033[K")
		}
	} else if message != "" {
		// Spinner wasn't active but we have a message to print
		fmt.Println(message)
	}
}

// UpdateMessage changes the spinner message while running.
func (s *Spinner) UpdateMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}

// WithSpinner runs a function with a spinner, handling success/failure messages.
func WithSpinner(message string, fn func() error) error {
	spinner := NewSpinner(message)
	spinner.Start()
	err := fn()
	if err != nil {
		spinner.StopWithMessage(fmt.Sprintf("✗ %s failed", message))
		return err
	}
	spinner.StopWithMessage(fmt.Sprintf("✓ %s", message))
	return nil
}
