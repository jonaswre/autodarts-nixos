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
	referenceInterval := flag.Duration("world-reference-interval", 5*time.Minute, "Interval between stable physical-world observations")
	listen := flag.String("listen", "0.0.0.0:8090", "Dashboard listen address; use 127.0.0.1:8090 for local-only access")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	quota := *quotaGB << 30
	catalog, err := dataset.NewCatalog(*output, quota)
	if err != nil {
		log.Fatal(err)
	}
	recorder, err := dataset.New(dataset.Options{
		BoardURL: *boardURL, OutputDir: *output, Quota: quota, Catalog: catalog, PreDelay: *pre, PostDelay: *post,
		ReferenceInterval: *referenceInterval,
		OnSample: func(path string, label dataset.Label) {
			if label.HasCoordinates {
				log.Printf("saved %s: dart %d %s at x=%.16g y=%.16g (%d frames)", path, label.DartIndex, label.Segment.Name, label.Coordinates.X, label.Coordinates.Y, len(label.FrameSequence))
			} else {
				log.Printf("saved %s: %s (%d frames)", path, label.Supervision, len(label.FrameSequence))
			}
		},
		OnError: func(err error) { log.Printf("skipped sample: %v", err) },
	})
	if err != nil {
		log.Fatal(err)
	}
	dashboard := &http.Server{Addr: *listen, Handler: dataset.Dashboard{OutputDir: *output, Quota: quota, Catalog: catalog}, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("dataset dashboard: http://%s", *listen)
		if err := dashboard.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dashboard: %v", err)
			stop()
		}
	}()
	defer dashboard.Shutdown(context.Background())
	log.Printf("recording multi-camera training data from %s into %s", *boardURL, *output)
	if err := recorder.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
