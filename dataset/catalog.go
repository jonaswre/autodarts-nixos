package dataset

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type storedSample struct {
	entry         SampleEntry
	trainingReady bool
}

// Catalog scans existing samples once at process startup and then tracks
// additions and quota removals incrementally. Dashboard refreshes never need
// to walk every image in the dataset.
type Catalog struct {
	mu        sync.RWMutex
	outputDir string
	quota     int64
	usedBytes int64
	samples   map[string]storedSample
}

func NewCatalog(outputDir string, quota int64) (*Catalog, error) {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return nil, err
	}
	stored, used, err := scanStoredSamples(outputDir)
	if err != nil {
		return nil, err
	}
	catalog := &Catalog{outputDir: outputDir, quota: quota, usedBytes: used, samples: make(map[string]storedSample, len(stored))}
	for _, sample := range stored {
		catalog.samples[sample.entry.Label.SampleID] = sample
	}
	return catalog, nil
}

func (c *Catalog) Inventory() Inventory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := Inventory{UsedBytes: c.usedBytes, QuotaBytes: c.quota, Samples: []SampleEntry{}}
	for _, sample := range c.samples {
		if sample.trainingReady {
			result.Samples = append(result.Samples, sample.entry)
		} else {
			result.EvidenceCount++
			result.EvidenceBytes += sample.entry.Size
		}
	}
	sort.Slice(result.Samples, func(i, j int) bool {
		return result.Samples[i].Label.CapturedAt.After(result.Samples[j].Label.CapturedAt)
	})
	result.SampleCount = len(result.Samples)
	return result
}

func (c *Catalog) AddPath(label Label, directory string) error {
	if !safeName(label.SampleID) || filepath.Clean(directory) != filepath.Join(filepath.Clean(c.outputDir), label.SampleID) {
		return errors.New("sample directory does not match its safe identity")
	}
	size, err := directorySize(directory)
	if err != nil {
		return err
	}
	entry := SampleEntry{Label: label, Size: size, PreviewDarts: previewDartsForLabel(c.outputDir, label)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.samples[label.SampleID]; ok {
		c.usedBytes -= existing.entry.Size
	}
	c.samples[label.SampleID] = storedSample{entry: entry, trainingReady: isTrainingReady(label)}
	c.usedBytes += size
	return nil
}

func (c *Catalog) EnforceQuota() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quota <= 0 || c.usedBytes <= c.quota {
		return nil
	}
	names := make([]string, 0, len(c.samples))
	for name := range c.samples {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if c.usedBytes <= c.quota {
			break
		}
		sample := c.samples[name]
		if err := os.RemoveAll(filepath.Join(c.outputDir, name)); err != nil {
			return err
		}
		delete(c.samples, name)
		c.usedBytes -= sample.entry.Size
	}
	return nil
}

func scanStoredSamples(outputDir string) ([]storedSample, int64, error) {
	entries, err := os.ReadDir(outputDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var result []storedSample
	var used int64
	for _, entry := range entries {
		if !entry.IsDir() || !safeName(entry.Name()) {
			continue
		}
		directory := filepath.Join(outputDir, entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, "label.json"))
		if err != nil {
			continue
		}
		var label Label
		if json.Unmarshal(data, &label) != nil || label.SampleID != entry.Name() {
			continue
		}
		normalizeLegacyLabel(&label)
		size, err := directorySize(directory)
		if err != nil {
			continue
		}
		used += size
		result = append(result, storedSample{
			entry:         SampleEntry{Label: label, Size: size, PreviewDarts: previewDartsForLabel(outputDir, label)},
			trainingReady: isTrainingReady(label),
		})
	}
	return result, used, nil
}

func directorySize(directory string) (int64, error) {
	var size int64
	err := filepath.WalkDir(directory, func(_ string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !item.IsDir() {
			info, err := item.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func previewDartsForLabel(outputDir string, label Label) *PreviewDarts {
	setup, err := loadPreviewSetup(outputDir, label)
	if err != nil {
		return nil
	}
	before := label.WorldBefore
	if before == nil {
		before = label.WorldAfter
	}
	value := PreviewDarts{}
	if before != nil {
		value.Before = projectPreviewDarts(setup, before.Darts)
	}
	if label.WorldAfter != nil {
		value.After = projectPreviewDarts(setup, label.WorldAfter.Darts)
	}
	if len(value.Before) == 0 && len(value.After) == 0 {
		return nil
	}
	return &value
}
