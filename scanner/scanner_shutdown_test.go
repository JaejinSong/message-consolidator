package scanner

import (
	"context"
	"testing"
	"time"
)

const shutdownTestTimeout = 5 * time.Second

func TestStartBackgroundScanner_ReturnsPromptlyWithoutBlocking(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan (<-chan struct{}), 1)

	// Act
	go func() { returned <- StartBackgroundScanner(ctx) }()

	// Assert
	select {
	case done := <-returned:
		if done == nil {
			t.Fatal("expected a non-nil done channel")
		}
		cancel()
		<-done
	case <-time.After(shutdownTestTimeout):
		t.Fatal("StartBackgroundScanner blocked instead of returning")
	}
}

func TestStartBackgroundScanner_DoneStaysOpenWhileContextIsLive(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Act
	done := StartBackgroundScanner(ctx)

	// Assert
	select {
	case <-done:
		t.Fatal("done channel closed while ctx was still live")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	<-done
}

func TestStartBackgroundScanner_DoneClosesAfterContextCancel(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	done := StartBackgroundScanner(ctx)

	// Act
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(shutdownTestTimeout):
		t.Fatal("done channel did not close after ctx cancel")
	}
}
