package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jonaswre/autodarts-nixos/dataset"
)

func main() {
	boardURL := flag.String("url", "http://autodarts:3180", "Board Manager URL")
	output := flag.String("output", "./autodarts-dataset", "Local dataset directory")
	quotaGB := flag.Int64("quota-gb", 20, "Maximum local dataset size in GiB")
	pre := flag.Duration("before", 250*time.Millisecond, "Frame offset before an accepted dart")
	post := flag.Duration("after", 350*time.Millisecond, "Frame offset after an accepted dart")
	listen := flag.String("listen", "0.0.0.0:8090", "Dashboard listen address; use 127.0.0.1:8090 for local-only access")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	recorder, err := dataset.New(dataset.Options{
		BoardURL: *boardURL, OutputDir: *output, Quota: *quotaGB << 30, PreDelay: *pre, PostDelay: *post,
		OnSample: func(path string, label dataset.Label) {
			log.Printf("saved %s: dart %d %s at x=%.16g y=%.16g", path, label.DartIndex, label.Segment.Name, label.Coordinates.X, label.Coordinates.Y)
		},
		OnError: func(err error) { log.Printf("skipped sample: %v", err) },
	})
	if err != nil {
		log.Fatal(err)
	}
	dashboard := &http.Server{Addr: *listen, Handler: dataset.Dashboard{OutputDir: *output, Quota: *quotaGB << 30}, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("dataset dashboard: http://%s", *listen)
		if err := dashboard.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dashboard: %v", err)
			stop()
		}
	}()
	defer dashboard.Shutdown(context.Background())
	log.Printf("recording accepted darts from %s into %s", *boardURL, *output)
	if err := recorder.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
