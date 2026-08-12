package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

func main() {
	boardURL := flag.String("url", "http://autodarts:3180", "Board Manager URL")
	once := flag.Bool("once", false, "print the current board state and exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := autodarts.New(*boardURL)
	if err != nil {
		log.Fatal(err)
	}
	initial, err := client.BoardState(ctx)
	if err != nil {
		log.Fatal(err)
	}
	printState(initial)
	if *once {
		return
	}
	events, failures, err := client.Events(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			changed, err := client.ApplyBoardEvent(event)
			if err != nil {
				log.Printf("event: %v", err)
				continue
			}
			if !changed {
				continue
			}
			state := client.Snapshot().Board
			printState(state)
		case err := <-failures:
			if err != nil {
				log.Fatal(err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

func printState(state autodarts.BoardState) {
	fmt.Printf("board status=%q event=%q throws=%d", state.Status, state.Event, state.NumThrows)
	for index, dart := range state.Throws {
		fmt.Printf(" dart%d=%s(x=%.4f,y=%.4f)", index+1, dart.Segment.Name, dart.Coordinates.X, dart.Coordinates.Y)
	}
	fmt.Println()
}
